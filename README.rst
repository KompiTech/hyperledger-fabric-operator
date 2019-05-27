===========================
Hyperledger Fabric Operator
===========================

**NOTE: This project is in pre-alpha**

Kubernetes operator for hyperledger fabric. This project is using Kubernetes Custom Resource Definition
(more information https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
to manage Fabric Peers and Orderers in Kubernetes.


Installation
------------

CRD needs to be applied into the k8s cluster first.

.. code:: bash

    kubectl apply -f deploy/crds/

Operator deployment with RBAC

.. code:: bash

    kubectl apply -f deploy/rbac.yaml
    kubectl apply -f deploy/operator.yaml


User guide
----------

Pre-requirements:
=================

Currently Hyperledger Fabric Operator support only one use case which requires that Kubernetes has
deployed HashiCorp Vault, Istio and CoreDNS.

HashiCorp Vault is used for issuing signing certificate and key which are used in MSP for peers and orderers. TLS certificate and key is also issued from Vault. Currenly there is init
container which is using Vault Kubernetes Auth. Operator requires that HashiCorp Vault is properly configured. (https://www.vaultproject.io/docs/auth/kubernetes.html).
PKI in Vault should be configured with roles MSP and TLS.

Vault auth url is format `"$VAULT_ADDRESS"/v1/auth/kubernetes-"$REGION_NAME"/login`. Vault address could be changed via env variable OPERATOR_VAULT_ADDRESS in operator manifest.
You can use annotation in peer and orderer resources to define REGION_NAME.

.. code:: yaml

    apiVersion: hl-fabric.kompitech.com/v1alpha1
    kind: FabricPeer
    metadata:
    name: peer1
    namespace: 2657db63-8a32-41c6-814c-6fa3d21c4731
    annotations:
      region: Region1

Istio is needed because operator will create by default Ingress for created services.

Operator will also try to write dns record into Etcd. This etcd is backend for CoreDNS. Etcd address is currently staticly set to `etcd-client.etcd:2379`.


Cleanup
-------

If you want to delete all resources after you are done.

.. code:: bash

    kubectl delete -f deploy/operator.yaml
    kubectl delete -f deploy/rbac.yaml
    kubectl delete -f deploy/crds
