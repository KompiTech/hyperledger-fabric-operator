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

TODO - Example of Peers and Orderer manifests is in deploy/example_cr


Cleanup
-------

If you want to delete all resources after you are done.

.. code:: bash

    kubectl delete -f deploy/operator.yaml
    kubectl delete -f deploy/rbac.yaml
    kubectl delete -f deploy/crds
