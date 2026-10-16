/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command cilium-manifest generates internal/controller/cni-cilium.yaml, the
// Go template of the Cilium CNI addon, from the Cilium Helm chart.
//
// It renders the chart with helm for the four combinations of the template
// switches (Talos values, kube-proxy replacement), merges the renders into one
// Go text/template with {{ if }} blocks around the lines that differ, and
// checks that executing the template gives back each Helm render byte for
// byte. Requires helm in PATH.
//
//	go run ./hack/cilium-manifest -version 1.20.2 -out internal/controller/cni-cilium.yaml
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

const repo = "https://helm.cilium.io"

// Sentinel values rendered by helm in place of the API server endpoint, then
// replaced by template actions.
const (
	hostSentinel = "k8s-service-host.invalid"
	portSentinel = "61234"
)

// baseValues are the Helm values of every variant.
var baseValues = []string{"devices=eth0", "hubble.enabled=false"}

// templateData mirrors ciliumManifestData in internal/controller: the fields
// the generated template uses.
type templateData struct {
	Talos                bool
	KubeProxyReplacement bool
	K8sServiceHost       string
	K8sServicePort       string
}

func main() {
	version := flag.String("version", "", "Cilium chart version (e.g. 1.20.2)")
	out := flag.String("out", "internal/controller/cni-cilium.yaml", "generated template")
	flag.Parse()
	if *version == "" {
		fail(errors.New("-version is required"))
	}
	if err := run(*version, *out); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "cilium-manifest:", err)
	os.Exit(1)
}

func run(version, out string) error {
	talosValues, err := talosValues(version)
	if err != nil {
		return err
	}
	kprValues := []string{
		"kubeProxyReplacement=true",
		"k8sServiceHost=" + hostSentinel,
		"k8sServicePort=" + portSentinel,
	}

	renders := map[[2]bool]string{}
	for _, talos := range []bool{false, true} {
		for _, kpr := range []bool{false, true} {
			values := slices.Clone(baseValues)
			if talos {
				values = append(values, talosValues...)
			}
			if kpr {
				values = append(values, kprValues...)
			}
			if renders[[2]bool{talos, kpr}], err = helmTemplate(version, values); err != nil {
				return err
			}
		}
	}

	base := lines(renders[[2]bool{false, false}])
	body, err := merge(base,
		variant{cond: ".Talos", lines: lines(renders[[2]bool{true, false}])},
		variant{cond: ".KubeProxyReplacement", lines: lines(renders[[2]bool{false, true}])},
	)
	if err != nil {
		return err
	}
	if strings.Count(body, `"`+hostSentinel+`"`) == 0 || strings.Count(body, `"`+portSentinel+`"`) == 0 {
		return errors.New("API server endpoint sentinels not found in the kube-proxy replacement render")
	}
	body = strings.ReplaceAll(body, `"`+hostSentinel+`"`, `{{ printf "%q" .K8sServiceHost }}`)
	body = strings.ReplaceAll(body, `"`+portSentinel+`"`, `"{{ .K8sServicePort }}"`)
	if strings.Contains(body, hostSentinel) || strings.Contains(body, portSentinel) {
		return errors.New("unexpected API server endpoint sentinel left in the template")
	}

	manifest := header(version, talosValues, kprValues) + body

	// The template must give back each Helm render.
	tmpl, err := template.New("cilium").Parse(manifest)
	if err != nil {
		return fmt.Errorf("parse generated template: %w", err)
	}
	for key, want := range renders {
		data := templateData{
			Talos:                key[0],
			KubeProxyReplacement: key[1],
			K8sServiceHost:       hostSentinel,
			K8sServicePort:       portSentinel,
		}
		var got bytes.Buffer
		if err := tmpl.Execute(&got, data); err != nil {
			return fmt.Errorf("execute generated template: %w", err)
		}
		if got.String() != want {
			name := fmt.Sprintf("cilium-talos=%v-kpr=%v", key[0], key[1])
			_ = os.WriteFile(filepath.Join(os.TempDir(), name+".helm.yaml"), []byte(want), 0o644)
			_ = os.WriteFile(filepath.Join(os.TempDir(), name+".template.yaml"), got.Bytes(), 0o644)
			return fmt.Errorf("generated template does not reproduce the helm render %s: compare %s.{helm,template}.yaml in %s",
				name, name, os.TempDir())
		}
	}

	return os.WriteFile(out, []byte(manifest), 0o644)
}

// talosValues returns the Talos Linux values of the Talos Cilium guide: the
// chart default capabilities without SYS_MODULE (Talos does not let workloads
// load kernel modules), the cgroup v2 hierarchy mounted by Talos, and the
// Kubernetes IPAM mode.
func talosValues(version string) ([]string, error) {
	out, err := exec.Command("helm", "show", "values", "cilium", "--repo", repo, "--version", version).Output()
	if err != nil {
		return nil, fmt.Errorf("helm show values: %w", err)
	}
	var values struct {
		SecurityContext struct {
			Capabilities map[string][]string `yaml:"capabilities"`
		} `yaml:"securityContext"`
	}
	if err := yaml.Unmarshal(out, &values); err != nil {
		return nil, fmt.Errorf("parse chart values: %w", err)
	}
	set := []string{
		"ipam.mode=kubernetes",
		"cgroup.autoMount.enabled=false",
		"cgroup.hostRoot=/sys/fs/cgroup",
	}
	for _, key := range []string{"ciliumAgent", "cleanCiliumState"} {
		capabilities := values.SecurityContext.Capabilities[key]
		if !slices.Contains(capabilities, "SYS_MODULE") {
			return nil, fmt.Errorf("securityContext.capabilities.%s has no SYS_MODULE: review the Talos values", key)
		}
		capabilities = slices.DeleteFunc(slices.Clone(capabilities), func(c string) bool { return c == "SYS_MODULE" })
		set = append(set, fmt.Sprintf("securityContext.capabilities.%s={%s}", key, strings.Join(capabilities, ",")))
	}
	return set, nil
}

func helmTemplate(version string, values []string) (string, error) {
	args := make([]string, 0, 9+2*len(values))
	args = append(args, "template", "cilium", "cilium", "--repo", repo, "--version", version, "--namespace", "kube-system")
	for _, v := range values {
		args = append(args, "--set", v)
	}
	cmd := exec.Command("helm", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("helm template %v: %w", values, err)
	}
	return string(out), nil
}

func header(version string, talosValues, kprValues []string) string {
	// sets formats values as --set flags, one per line, each line but the
	// last continued with a backslash.
	sets := func(values []string) string {
		flags := make([]string, len(values))
		for i, v := range values {
			flags[i] = "    --set " + v
		}
		return strings.Join(flags, " \\\n")
	}
	return fmt.Sprintf(`{{- /*
Code generated by hack/cilium-manifest (make cilium-manifest). DO NOT EDIT.

Cilium %s, rendered from the Helm chart and executed with Go text/template by
the XOCluster controller (see ciliumManifestData):

  helm template cilium cilium --repo %s --version %s \
    --namespace kube-system \
%s

.Talos adds the Talos Linux values (chart default capabilities without
SYS_MODULE):
%s

.KubeProxyReplacement adds (the endpoint comes from .K8sServiceHost and
.K8sServicePort):
%s
*/ -}}
`, version, repo, version, sets(baseValues), sets(talosValues), sets(kprValues))
}

// variant is a render that differs from the base render when cond is true.
type variant struct {
	cond  string
	lines []string
}

// hunk replaces base[i1:i2] with the variant lines new when the variant
// condition is true.
type hunk struct {
	i1, i2 int
	cond   string
	new    []string
}

// merge builds the template body from the base render and the variants. The
// variants must change disjoint parts of the base render, so that any
// combination of them is the base render with the hunks of each enabled
// variant applied.
func merge(base []string, variants ...variant) (string, error) {
	var hunks []hunk
	for _, v := range variants {
		for _, h := range diff(base, v.lines) {
			h.cond = v.cond
			hunks = append(hunks, h)
		}
	}
	slices.SortFunc(hunks, func(a, b hunk) int { return a.i1 - b.i1 })
	for i := 1; i < len(hunks); i++ {
		if hunks[i].i1 <= hunks[i-1].i2 {
			return "", fmt.Errorf("%s and %s change overlapping lines around line %d: merge them by hand",
				hunks[i-1].cond, hunks[i].cond, hunks[i].i1+1)
		}
	}

	// The actions are written at the start of a line, without whitespace
	// trimming: "{{ if c }}" before the first conditional line and
	// "{{ end }}" before the line that follows the block. Every conditional
	// line keeps its own newline and blank lines are preserved.
	var out []string
	prefix := ""
	emit := func(ls []string) {
		for _, l := range escape(ls) {
			out = append(out, prefix+l)
			prefix = ""
		}
	}
	next := 0
	for _, h := range hunks {
		emit(base[next:h.i1])
		switch {
		case h.i1 == h.i2: // insertion
			prefix += "{{ if " + h.cond + " }}"
			emit(h.new)
		case len(h.new) == 0: // deletion
			prefix += "{{ if not " + h.cond + " }}"
			emit(base[h.i1:h.i2])
		default:
			prefix += "{{ if " + h.cond + " }}"
			emit(h.new)
			prefix += "{{ else }}"
			emit(base[h.i1:h.i2])
		}
		prefix += "{{ end }}"
		next = h.i2
	}
	emit(base[next:])
	if prefix != "" {
		return "", errors.New("a variant changes the end of the render: merge it by hand")
	}
	return strings.Join(out, "\n"), nil
}

// diff returns the hunks turning a into b, from a longest common subsequence
// of their lines.
func diff(a, b []string) []hunk {
	// lcs[i][j] is the LCS length of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	var hunks []hunk
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			i++
			j++
			continue
		}
		h := hunk{i1: i}
		j1 := j
		for i < len(a) || j < len(b) {
			if i < len(a) && j < len(b) && a[i] == b[j] {
				break
			}
			if j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]) {
				j++
			} else {
				i++
			}
		}
		h.i2 = i
		h.new = b[j1:j]
		hunks = append(hunks, h)
	}
	return hunks
}

// lines splits a render into lines; the trailing newline is kept as an empty
// last line so that joining with "\n" gives back the render.
func lines(s string) []string {
	return strings.Split(s, "\n")
}

// braceRun matches the runs of braces that may contain template delimiters.
var braceRun = regexp.MustCompile(`[{}]{2,}`)

// escape makes Helm output literal in a Go template: each run of braces
// containing "{{" or "}}" (e.g. "{}}}}" in the Envoy JSON configuration) is
// emitted as a quoted string action, so no brace stays next to a delimiter.
func escape(in []string) []string {
	out := make([]string, len(in))
	for i, l := range in {
		out[i] = braceRun.ReplaceAllStringFunc(l, func(run string) string {
			if !strings.Contains(run, "{{") && !strings.Contains(run, "}}") {
				return run
			}
			return "{{" + strconv.Quote(run) + "}}"
		})
	}
	return out
}
