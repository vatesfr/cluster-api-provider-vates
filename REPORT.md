# Rapport — Découplage de la version K8s de l'image VM

## Résumé des changements

**Suppression :**
- Packer (`examples/packers/`) — plus besoin de pré-construire des images VM avec K8s
- Prefilled ClusterClass + VatesMachineTemplates — redondant avec almalinux-fromscratch
- kernel-modules / kernel-modules-extra — inutiles (89 Mo), overlay et br_netfilter sont built-in

**Modifié :**
- CP et Workers utilisent le repo RPM Kubernetes (`pkgs.k8s.io/core:/stable:/v1.36/rpm/`)
- Kubelet/kubeadm/kubectl installés via `dnf` au bootstrap (pas de download dl.k8s.io)
- Aucune unité systemd custom créée — celle du RPM est utilisée
- Aucun flag `--config` ou `--kubeconfig` en dur
- kernel-modules supprimés des preKubeadmCommands

**Conservé (identique au main) :**
- inject.go original (pas de RBAC kube-vip)
- calicoPatch original (juste `IP_AUTODETECTION_METHOD`)
- kube-vip postScript original (génération du manifest après kubeadm init)
- containerd installé via EPEL

## Pourquoi ça marche (identique au main)

Le flot est le même que le almalinux-fromscratch du main :
1. VM boot → cloud-init
2. kernel modules, sysctls, swap
3. containerd installé via EPEL
4. Repo K8s configuré → kubelet/kubeadm installés via RPM
5. kubelet.service = celui du RPM (standard, `/usr/lib/systemd/system/kubelet.service`)
6. kubelet enable (sans --now pour le CP, avec --now pour le worker)
7. kubeadm init / kubeadm join
8. kube-vip postScript génère le manifest
9. kubelet démarre kube-vip

L'unité systemd standard du RPM a :
```
ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS $KUBELET_EXTRA_ARGS
EnvironmentFile=-/var/lib/kubelet/kubeadm-flags.env
```

Kubeadm init écrit `KUBELET_KUBEADM_ARGS="--cgroup-driver=systemd"` + `--config=` dans l'env file. Avec l'unité standard et les variables, kubelet charge correctement la config.

## Pourquoi nos tentatives de download dl.k8s.io ont échoué

1. **Unité systemd** : Nous devions créer une unité custom car RPM absent. Notre unité avait des flags hardcodés (`--config`, `--kubeconfig`) qui ne correspondaient pas au comportement attendu par kubeadm. kubeadm ne reconnaît pas qu'il doit redémarrer kubelet avec ces flags → kubelet se lance sans config → pas de static pods.

2. **RBAC kube-vip** : Le certificat admin.conf a `O=kubeadm:cluster-admins` (kubeadm v1.36.0 de dl.k8s.io), pas `O=system:masters`. Le cluster-admin ClusterRoleBinding ne lie que `system:masters`. Donc kubernetes-admin n'a aucun droit → kube-vip ne peut pas acquérir le leader election lease. Avec le RPM, le O est `system:masters`.

3. **Calico** : Par défaut, Calico essaie de joindre l'API via le service IP (`192.168.0.1:443`). Sans kube-proxy (si kubeadm init rate upload-config), c'est inaccessible. Et kube-proxy n'est pas déployé si upload-config échoue (car le VIP n'est pas encore actif pendant kubeadm init).

## Conclusion

Le repo RPM est la solution robuste. Il gère :
- L'installation du binaire kubelet standard
- L'unité systemd standard
- Les dépendances et la configuration
- La compatibilité avec kubeadm

Le découplage de l'image VM est atteint : l'image de base ne contient que containerd + xe-guest-utilities. Tout le reste (kubelet, kubeadm, kubectl) est installé au bootstrap depuis le repo RPM.
