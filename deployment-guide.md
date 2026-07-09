# DANM Deployment Guide

## Table of Contents


  * [Getting started](#getting-started)
      * [Prerequisites](#prerequisites)
      * [Building the binaries](#building-the-binaries)
  * [Deployment](#deployment)


## Getting started

### Prerequisites

To begin, you need to create your own Kubernetes cluster, and install DANM manually. We suggest
to use any of the automated Kubernetes installing solutions (kubeadm, minikube etc.) for a painless
experience.

We currently test DANM with Kubernetes 1.17.X. Compatibility with earlier than 1.9.X versions of
Kubernetes is not officially supported.

**Running with pre-1.15.X versions have known issues when used together with the production-grade
network management APIs. These originate from Kubernetes core code limitations.**

Best bet is to always stay up-to-date!

The project does not currently have a Docker container release, so we will walk you through the
entire process of building all artifacts from scratch. To be able to do that, your development
environment shall already have Docker daemon installed and ready to build containers.


### Building the binaries

It is actually as easy as cloning the repository from GitHub, and executing the `build_danm.sh`
script from the root of the project!

```
git clone github.com/danm-cni/danm
cd danm
./build_danm.sh
```

The result will four container images:

  - `danm-cni-plugins`: This image contains the core CNI plugins (`danm`, `fakeipam`). Later on,
    it will be deployed as a DaemonSet that puts these binaries in place in each Kubernetes node.

  - `netwatcher`: This image will be used by the `netwatcher` DaemonSet

  - `webhook`: This image will be used by the `webhook` deployment

  - `svcwatcher`: This image will be used by the `svcwatcher` DaemonSet if you choose to install it.


## Deployment

As a quicker but currently experimental option, please also take a look at
[Deploying using an installer job](deployment-installer-job.md). This option integrates all
of the steps mentioned below, into a single one-stop-shop installer. However, please treat
this option as experimental for now -- and only apply it on a Kubernetes cluster where you
feel comfortable with tolerating the impact if something goes wrong. Also, please let
us know any issues you encounter!

Otherwise, the manual method of deploying the whole DANM suite into a Kubernetes cluster is
the following:

### 1. Extend the Kubernetes API

There are two options to choose from:

 1. **Lightweight**: Extend the Kubernetes API with the `DanmNet` and `DanmEp` CRD objects for a
    simplified network management experience by executing the following command from the project's
    root directory:

    ```
    kubectl create -f integration/crds/lightweight
    ```

 1. **Production**: Extend the Kubernetes API with the `TenantNetwork`, `ClusterNetwork`,
    `TenantConfig`, and `DanmEp` CRD objects for a multi-tenant capable, production-grade network
    management experience by executing the following command from the project's root directory:

    ```
    kubectl create -f integration/crds/production
    ```

### 2. Create a service account for the DANM CNI

In order to do its job, DANM needs a service account to access the cluster, and for that account to
have the necessary RBAC roles provisioned.

We also need to extract the token for this service account, as it will be required in the next step:

```
kubectl create --namespace kube-system serviceaccount danm
SECRET_NAME=$(kubectl get --namespace kube-system -o jsonpath='{.secrets[0].name}' serviceaccounts danm)
SERVICEACCOUNT_TOKEN=$(kubectl get --namespace kube-system secrets ${SECRET_NAME} -o jsonpath='{.data.token}' | base64 -d)
```

### 3. Create a valid CNI configuration file

Put a valid CNI config file into the CNI configuration directory of all your kubelet nodes (by
default it is `/etc/cni/net.d/`), based on the following ecxample configuration:

[Example CNI config file](https://github.com/danm-cni/danm/tree/master/integration/cni_config/00-danm.conf)

As kubelet considers the first .conf file in the configured directory as the valid CNI config of the
cluster, it is generally a good idea to prefix the .conf file of any CNI metaplugin with "00".
Make sure to configure the optional DANM configuration parameters to match your environment!
The parameter `kubeconfig` is mandatory, and shall point to a valid kubeconfig file.

In order to create a valid kubeconfig file, the cluster server and CA certificate need to be known:

```
CLUSTER_NAME=$(kubectl config view -o jsonpath='{.clusters[0].name}')
CLUSTER_SERVER=$(kubectl config view -o jsonpath='{.clusters[0].cluster.server}')
CLUSTER_CA_CERTIFICATE=$(kubectl config view --flatten -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
```
*(note: Above commands may not work if you have more than one cluster in your kubeconfig file. In
that case, adjust the commands above to pick the correct cluster, or obtain the values manually)*

With both the service account token from step 2, and the cluster information from just above,
a kubeconfig file can be created. If you ran the commands as show above, this is now simply
a matter of replacing the variables either manually or with a tool like `envsubst`.

[Example kubeconf file](https://github.com/danm-cni/danm/tree/master/integration/cni_config/example_kubeconfig.yaml)

Also provision the necessary RBAC rules so DANM can do its job:

```
kubectl create -f integration/cni_config/danm_rbac.yaml
```

### 4. Onboard container images

Onboard the netwatcher, svcwatcher, and webhook containers into the image registry of your cluster


### 5. Create CNI plugin DaemonSet

Create the cni-plugin DaemonSet by executing the following command from the project's root
directory:

```
kubectl create -f integration/cni_config/danm_rbac.yaml
kubectl create -f integration/manifests/cni_plugins
```

This DaemonSet will copy the `danm` and `fakeipam` binaries into the `/opt/cni/bin` directory of
each node.


### 6. (OPTIONAL): Install other CNI plugins

Install other CNI plugins (flannel, sriov etc.) you would like to use in your cluster.

Specific installation steps depend on the CNI plugin; some require copying into `/opt/cni/bin` on
all nodes in your cluster, whereas others are installed using a DaemonSet (or a combination of both).


### 7. Create the NetWatcher DaemonSet

Create the netwatcher DaemonSet by executing the following command from the project's root
directory:

```
kubectl create -f integration/manifests/netwatcher/
```

Notes:

  - you should take a look at the example manifest, and possibly tailor it to your own environment
    first

  -  we assume RBAC is configured for the Kubernetes API, so the manifests include the required
     Role and ServiceAccount for this case.


### 8. Create a bootstrap network

**Create at least one DANM network to bootstrap your infrastructure Pods!**

Otherwise you can easily fall into a catch 22 situation - you won't be able to bring-up Pods because
you don't have network, but you cannot create networks because you cannot bring-up a Pod to validate
them.

Your bootstrap networking solution can be really anything you fancy!

We use Flannel or Calico for the purpose in our environments, and connect Pods to it with such
simple network descriptors like what you can find in `integration/bootstrap_networks`.

### 9. Create the Webhook Deployment

Create the webhook Deployment and provide it with certificates by executing the following commands
from the project's root directory:

Below scripts require the `jq` tool and `openssl`; please make sure you have them installed.

```
./integration/manifests/webhook/webhook-create-signed-cert.sh \
   cat ./integration/manifests/webhook/webhook.yaml | \
   ./integration/manifests/webhook/webhook-patch-ca-bundle.sh > \
   ./integration/manifests/webhook/webhook-ca-bundle.yaml
kubectl create -f integration/manifests/webhook/webhook-ca-bundle.yaml
```

**Disclaimer**: Webhook already leverages DANM CNI to create its network interface. Don't forget to
change the name of the network referenced in the example manifest file to your bootstrap network!

We also assume RBAC is configured in your cluster.


***You are now ready to use the services of DANM, and can start bringing-up Pods within your
cluster!***


### 10. (OPTIONAL) Create the Svcwatcher deployment

Create the svcwatcher Deployment by executing the following command from the project's root directory:

```
kubectl create -f integration/manifests/svcwatcher/
```

This component is an optional part of the suite. You only need to install it if you would like to
use Kubernetes Services for all the network interfaces of your Pod - but who wouldn't want that??

**Disclaimer**: Svcwatcher, and webhook already leverage DANM CNI to create their network interface.
Don't forget to configure an appropriate default network in your cluster before you instantiate
them!

We use Flannel, or Calico for this purpose in our infrastructures.

We also assume RBAC is configured in your cluster.
