/*
   Copyright 2019 KompiTech GmbH

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

package fabricpeer

import (
	"context"
	"reflect"

	"github.com/imdario/mergo"

	"github.com/KompiTech/hl-fabric-operator/pkg/config"
	"github.com/KompiTech/hl-fabric-operator/pkg/resources"
	"k8s.io/apimachinery/pkg/util/intstr"

	fabricv1alpha1 "github.com/KompiTech/hl-fabric-operator/pkg/apis/fabric/v1alpha1"
	crd "github.com/jiribroulik/pkg/apis/istio/v1alpha3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	peerMSPPath = "/etc/hyperledger/fabric/msp/"
	peerTLSPath = "/etc/hyperledger/fabric/tls/"
)

var log = logf.Log.WithName("controller_fabricpeer")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new FabricPeer Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFabricPeer{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("fabricpeer-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource FabricPeer
	err = c.Watch(&source.Kind{Type: &fabricv1alpha1.FabricPeer{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource StatefulSet and requeue the owner FabricPeer
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &fabricv1alpha1.FabricPeer{},
	})
	if err != nil {
		return err
	}

	// // Watch for changes to secondary resource Pods and requeue the owner FabricPeer
	// err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
	// 	IsController: true,
	// 	OwnerType:    &appsv1.StatefulSet{},
	// })
	// if err != nil {
	// 	return err
	// }

	return nil
}

var _ reconcile.Reconciler = &ReconcileFabricPeer{}

// ReconcileFabricPeer reconciles a FabricPeer object
type ReconcileFabricPeer struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a FabricPeer object and makes changes based on the state read
// and what is in the FabricPeer.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileFabricPeer) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling FabricPeer")

	reqLogger.Info("Reconciling FabricPeer", "vaultAddress", config.VaultAddress)

	// Fetch the FabricPeer instance
	instance := &fabricv1alpha1.FabricPeer{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	//Set global namespace
	namespace := instance.GetNamespace()

	//Create namespace
	newServiceAccount := resources.NewServiceAccount("vault", namespace)
	currentServiceAccount := &corev1.ServiceAccount{}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: "vault", Namespace: namespace}, currentServiceAccount)
	if err != nil && errors.IsNotFound(err) {
		//Secret not exists
		reqLogger.Info("Creating a new service account", "Namespace", newServiceAccount.GetNamespace(), "Name", newServiceAccount.GetName())
		err = r.client.Create(context.TODO(), newServiceAccount)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	//Create secrets for peers with certificates`
	for key, secretData := range instance.Spec.Certificate {
		secretName := instance.Name + "-" + key
		data := make(map[string][]byte)
		for _, item := range secretData {
			data[item.Name] = []byte(item.Value)
		}
		newSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: data,
		}

		// Set FabricPeer instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, newSecret, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		currentSecret := &corev1.Secret{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: newSecret.Name, Namespace: newSecret.Namespace}, currentSecret)
		if err != nil && errors.IsNotFound(err) {
			//Secret not exists
			reqLogger.Info("Creating a new secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
			err = r.client.Create(context.TODO(), newSecret)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else if err != nil {
			return reconcile.Result{}, err
		}
		// Updating secrets
		eq := reflect.DeepEqual(newSecret.Data, currentSecret.Data)
		if !eq {
			reqLogger.Info("Updating secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
			err = r.client.Update(context.TODO(), newSecret)
			if err != nil {
				return reconcile.Result{}, err
			}
		}

	}

	//Create sts for peer
	// Define a new Statef object
	newSts := newPeerStatefulSet(instance)
	pvcs := []corev1.PersistentVolumeClaim{}

	for _, item := range newPeerVolumeClaimTemplates(instance) {
		pvc := item
		if err := controllerutil.SetControllerReference(instance, &pvc, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
		pvcs = append(pvcs, pvc)

	}
	newSts.Spec.VolumeClaimTemplates = pvcs

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newSts, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this StatefulSet already exists
	currentSts := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new StatefulSet", "Namespace", newSts.Namespace, "Name", newSts.Name)
		err = r.client.Create(context.TODO(), newSts)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	candidate := currentSts.DeepCopy()

	if !reflect.DeepEqual(currentSts.Spec.Replicas, instance.Spec.Replicas) {
		candidate.Spec.Replicas = &instance.Spec.Replicas
	}

	for i, current := range candidate.Spec.Template.Spec.Containers {
		for j, new := range newSts.Spec.Template.Spec.Containers {
			if current.Name == new.Name {
				if !reflect.DeepEqual(current.Image, new.Image) {
					if err := mergo.Merge(&candidate.Spec.Template.Spec.Containers[i].Image, newSts.Spec.Template.Spec.Containers[j].Image, mergo.WithOverride); err != nil {
						reqLogger.Info("MERGE changes failed!!!", "Namespace", candidate.Namespace, "Name", candidate.Name, "Error", err.Error())
					}
				}
				if !reflect.DeepEqual(current.Resources, new.Resources) {
					if err := mergo.Merge(&candidate.Spec.Template.Spec.Containers[i].Resources, newSts.Spec.Template.Spec.Containers[j].Resources, mergo.WithOverride); err != nil {
						reqLogger.Info("MERGE changes failed!!!", "Namespace", candidate.Namespace, "Name", candidate.Name, "Error", err.Error())
					}
				}
			}
		}
	}

	reqLogger.Info("CANDIDATE", "Namespace", candidate.Spec)
	reqLogger.Info("CURRENT", "Namespace", currentSts.Spec)

	if !reflect.DeepEqual(candidate.Spec, currentSts.Spec) {
		reqLogger.Info("UPDATING peer statefulset!!!", "Namespace", candidate.Namespace, "Name", candidate.Name)
		err = r.client.Update(context.TODO(), candidate)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		reqLogger.Info("NOTHING to update!!!")
	}

	//Create Service
	newService := newPeerService(instance)

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newService, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	currentService := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", newService.Namespace, "Name", newService.Name)
		err = r.client.Create(context.TODO(), newService)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = resources.CheckDNS(currentService.Spec.ClusterIP, currentService.GetObjectMeta().GetAnnotations()["fqdn"])
	if err != nil {
		reqLogger.Error(err, "failed check/update dns", "Namespace", instance.Namespace, "Name", instance.Name, "ServiceIP", currentService.Spec.ClusterIP, "CurrentFQDN", currentService.GetObjectMeta().GetAnnotations()["fqdn"])
		return reconcile.Result{}, err
	}

	//Create Gateway
	gatewayTemplate := resources.GatewayTemplate{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
		Servers:   resources.GetPeerServerPorts(instance.Spec.CommonName),
	}
	newGateway := resources.NewGateway(gatewayTemplate)

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newGateway, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Gateway already exists
	currentGateway := &crd.Gateway{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: newGateway.Name, Namespace: newGateway.Namespace}, currentGateway)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Gateway", "Namespace", newGateway.Namespace, "Name", newGateway.Name)
		err = r.client.Create(context.TODO(), newGateway)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	//Crate Virtual Service
	vsvcTemplate := resources.VirtualServiceTemplate{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
		Spec:      resources.GetPeerVirtualServiceSpec(instance.GetName(), instance.Spec.CommonName),
	}
	newVirtualService := resources.NewVirtualService(vsvcTemplate)

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newVirtualService, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Gateway already exists
	currentVirtualService := &crd.VirtualService{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: newVirtualService.Name, Namespace: newVirtualService.Namespace}, currentVirtualService)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Istio Virtual Service", "Namespace", newVirtualService.Namespace, "Name", newVirtualService.Name)
		err = r.client.Create(context.TODO(), newVirtualService)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	//Update CR status
	pod := &corev1.Pod{}
	//labelSelector := labels.SelectorFromSet(newPeerLabels(instance))
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Name + "-0", Namespace: instance.Namespace}, pod)
	if err != nil {
		if instance.Spec.Replicas != int32(0) {
			reqLogger.Error(err, "failed to get pods", "Namespace", instance.Namespace, "Name", instance.Name)
			return reconcile.Result{}, err
		}
		peerState := fabricv1alpha1.StateSuspended
		reqLogger.Info("Update fabric peer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", peerState)
		instance.Status.FabricPeerState = peerState
		err := r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update fabric peer status")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	peerState := ""

	if pod.Status.Phase != "Running" {
		peerState = fabricv1alpha1.StateCreating
	} else {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "peer" {
				if status.State.Running != nil {
					peerState = "Running"
				} else if status.RestartCount > 0 {
					peerState = "Error"
				}
			}
		}
	}

	// Update status.Nodes if needed
	if !reflect.DeepEqual(peerState, instance.Status.FabricPeerState) {
		reqLogger.Info("Update fabricpeer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", peerState)
		instance.Status.FabricPeerState = peerState
		err := r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric peer status")
			return reconcile.Result{}, err
		}
	}

	// Pod already exists - don't requeue
	reqLogger.Info("Reconcile: done", "Namespace", instance.Namespace, "Name", instance.Name)
	return reconcile.Result{}, nil
}

// newStatefulSetForPeer returns a StatefulSet for FabricPeer with the same name/namespace as the cr
func newPeerStatefulSet(cr *fabricv1alpha1.FabricPeer) *appsv1.StatefulSet {
	// newPeerCluster sts for fabric peer cluster
	replicas := cr.Spec.Replicas

	labels := newPeerLabels(cr)

	init := resources.GetInitContainer(resources.VaultInit{
		Organization: cr.Spec.Organization,
		CommonName:   cr.Spec.CommonName,
		VaultAddress: config.VaultAddress,
		TLSPath:      peerTLSPath,
		MSPPath:      peerMSPPath,
		Cluster:      cr.GetAnnotations()["region"],
		NodeType:     "peer",
	}) //TODO

	init = append(init, newCouchdbInit())

	sts := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: cr.Name,
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "vault",
					InitContainers:     init,
					Containers:         newPeerContainers(cr),
					Volumes:            newPeerVolumes(cr),
				},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			//Volume Claims templates
			VolumeClaimTemplates: newPeerVolumeClaimTemplates(cr),
		},
	}

	return sts
}

// newServiceForPeer returns a service for FabricPeer with the same name/namespace as the cr
func newPeerContainers(cr *fabricv1alpha1.FabricPeer) []corev1.Container {
	var user int64
	user = 0
	privileged := true
	procMount := corev1.DefaultProcMount

	baseContainers := []corev1.Container{
		{
			Name:            "peer",
			Image:           cr.Spec.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			//workingDir
			WorkingDir: "/opt/gopath/src/github.com/hyperledger/fabric/peer",
			Ports: []corev1.ContainerPort{
				{
					Name:          "containerport1",
					Protocol:      "TCP",
					ContainerPort: int32(7051),
				},
				{
					Name:          "containerport2",
					ContainerPort: int32(7052),
					Protocol:      "TCP",
				},
				{
					Name:          "containerport3",
					ContainerPort: int32(7053),
					Protocol:      "TCP",
				},
			},
			//command
			Command: []string{
				"/bin/sh",
			},
			//args
			Args: []string{
				"-c",
				"peer node start",
			},

			LivenessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: int32(30),
				PeriodSeconds:       int32(5),
			},
			ReadinessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: int32(10),
				PeriodSeconds:       int32(5),
				FailureThreshold:    int32(25),
			},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("300Mi"),
					corev1.ResourceCPU:    resource.MustParse("200m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("300Mi"),
					corev1.ResourceCPU:    resource.MustParse("200m"),
				},
			},

			//Volume mount
			VolumeMounts:             newPeerVolumeMounts(cr),
			TerminationMessagePath:   "/dev/termination-log",
			TerminationMessagePolicy: "File",
			//ENV
			Env: newPeerContainerEnv(cr),
		},
		{
			Name:            "couchdb",
			Image:           "hyperledger/fabric-couchdb:0.4.14",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports: []corev1.ContainerPort{
				{
					Name:          "containerport",
					ContainerPort: int32(5984),
					Protocol:      "TCP",
				},
			},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("500Mi"),
					corev1.ResourceCPU:    resource.MustParse("200m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("500Mi"),
					corev1.ResourceCPU:    resource.MustParse("200m"),
				},
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser: &user,
				ProcMount: &procMount,
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "peerdata",
					MountPath: "/opt/couchdb/data",
					SubPath:   "data/couchdb",
				},
			},
		},
		{
			Name:            "dind",
			Image:           "docker:18.09.3-dind",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: &corev1.SecurityContext{
				Privileged: &privileged,
				ProcMount:  &procMount,
			},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1500Mi"),
					corev1.ResourceCPU:    resource.MustParse("500m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1500Mi"),
					corev1.ResourceCPU:    resource.MustParse("500m"),
				},
			},
			TerminationMessagePath:   "/dev/termination-log",
			TerminationMessagePolicy: "File",
		},
	}

	if cr.Spec.Containers != nil {
		for _, c := range cr.Spec.Containers {
			for _, cBase := range baseContainers {
				if c.Name == cBase.Name {
					if err := mergo.Merge(&cBase, c, mergo.WithOverride); err != nil {
						//Handle error
					}
				}
			}
		}
	}

	return baseContainers
}

func newPeerContainerEnv(cr *fabricv1alpha1.FabricPeer) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{
			Name:  "DOCKER_HOST",
			Value: "127.0.0.1",
		},
		{
			Name:  "CORE_LEDGER_STATE_STATEDATABASE",
			Value: "CouchDB",
		},
		{
			Name:  "CORE_LEDGER_STATE_COUCHDBCONFIG_COUCHDBADDRESS",
			Value: "localhost:5984",
		},
		{
			Name:  "CORE_VM_ENDPOINT",
			Value: "http://127.0.0.1:2375",
		},
		{
			Name:  "CORE_VM_DOCKER_ATTACHSTDOUT",
			Value: "true",
		},
		{
			Name:  "CORE_LOGGING_SPEC",
			Value: "info",
		},
		{
			Name:  "CORE_METRICS_PROVIDER",
			Value: "prometheus",
		},
		{
			Name:  "CORE_METRICS_PROMETHEUS_HANDLERPATH",
			Value: "/metrics",
		},
		{
			Name:  "CORE_OPERATIONS_LISTENADDRESS",
			Value: "0.0.0.0:8080",
		},
		{
			Name:  "CORE_PEER_TLS_ENABLED",
			Value: "true",
		},
		{
			Name:  "CORE_PEER_GOSSIP_USELEADERELECTION",
			Value: "true",
		},
		{
			Name:  "CORE_PEER_GOSSIP_ORGLEADER",
			Value: "false",
		},
		{
			Name:  "CORE_PEER_PROFILE_ENABLED",
			Value: "true",
		},
		{
			Name:  "CORE_PEER_MSPCONFIGPATH",
			Value: "/etc/hyperledger/fabric/msp",
		},
		{
			Name:  "CORE_PEER_TLS_CERT_FILE",
			Value: "/etc/hyperledger/fabric/tls/cert.crt",
		},
		{
			Name:  "CORE_PEER_TLS_KEY_FILE",
			Value: "/etc/hyperledger/fabric/tls/cert.key",
		},
		{
			Name:  "FABRIC_CA_CLIENT_TLS_CERTFILES",
			Value: "/etc/hyperledger/fabric/tls/ca.crt",
		},
		{
			Name:  "CORE_PEER_TLS_ROOTCERT_FILE",
			Value: "/etc/hyperledger/fabric/tls/ca.crt",
		},
		{
			Name:  "CORE_PEER_TLS_CLIENTAUTHREQUIRED",
			Value: "false",
		},
		{
			Name:  "CORE_PEER_TLS_CLIENTROOTCAS_FILES",
			Value: "/etc/hyperledger/fabric/tls/ca.crt",
		},
		{
			Name:  "CORE_PEER_TLS_CLIENTCERT_FILE",
			Value: "/etc/hyperledger/fabric/tls/cert.crt",
		},
		{
			Name:  "CORE_PEER_TLS_CLIENTKEY_FILE",
			Value: "/etc/hyperledger/fabric/tls/cert.key",
		},
		{
			Name:  "ORG_ADMIN_CERT",
			Value: "/etc/hyperledger/fabric/msp/admincerts/cert.pem",
		},
		{
			Name:  "CORE_PEER_ID",
			Value: cr.Spec.CommonName,
		},
		{
			Name:  "CORE_PEER_ADDRESS",
			Value: "$(CORE_PEER_ID):7051",
		},
		{
			Name:  "PEER_HOME",
			Value: "/opt/gopath/src/github.com/hyperledger/fabric/peer",
		},
		{
			Name:  "ORG_NAME",
			Value: cr.Spec.Organization,
		},
		{
			Name:  "CORE_PEER_GOSSIP_EXTERNALENDPOINT",
			Value: "$(CORE_PEER_ID):7051",
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "status.podIP",
				},
			},
		},
		{
			Name:  "CORE_PEER_CHAINCODEADDRESS",
			Value: "$(POD_IP):7052",
		},
		{
			Name:  "CORE_PEER_CHAINCODELISTENADDRESS",
			Value: "0.0.0.0:7052",
		},
		{
			Name:  "CORE_PEER_LOCALMSPID",
			Value: cr.Spec.MspId,
		},
		{
			Name:  "GODEBUG",
			Value: "netdns=go",
		},
		{
			Name:  "CORE_CHAINCODE_BUILDER",
			Value: "smolaon/fabric-ccenv:amd64-2.0.0-snapshot-e77813c85",
		},
		{
			Name:  "CORE_CHAINCODE_GOLANG_RUNTIME",
			Value: "smolaon/fabric-baseos:amd64-2.0.0-snapshot-e77813c85",
		},
	}
	if cr.Spec.BootstrapNodeAddress != "" {
		env = append(env, corev1.EnvVar{
			Name:  "CORE_PEER_GOSSIP_BOOTSTRAP",
			Value: cr.Spec.BootstrapNodeAddress,
		})
	}

	return env
}

// newServiceForPeer returns a service for FabricPeer with the same name/namespace as the cr
func newPeerService(cr *fabricv1alpha1.FabricPeer) *corev1.Service {
	annotations := make(map[string]string)

	annotations["fqdn"] = cr.Spec.CommonName
	annotations["prometheus.io/scrape"] = "true"
	annotations["prometheus.io/port"] = "8080"

	var svcObjectMeta metav1.ObjectMeta
	var svcSpec corev1.ServiceSpec
	svcObjectMeta = metav1.ObjectMeta{
		Name:        cr.GetName(),
		Namespace:   cr.GetNamespace(),
		Labels:      newPeerLabels(cr),
		Annotations: annotations,
	}

	svcSpec = corev1.ServiceSpec{
		Selector: newPeerLabels(cr),
		Type:     cr.Spec.SvcType,
		Ports: []corev1.ServicePort{
			{
				Name:       "grpc-ext-listen-endpoint",
				Protocol:   "TCP",
				Port:       int32(7051),
				TargetPort: intstr.FromInt(int(7051)),
			},
			{
				Name:       "grpc-chaincode-listen",
				Protocol:   "TCP",
				Port:       int32(7052),
				TargetPort: intstr.FromInt(int(7052)),
			},
			{
				Name:       "metrics",
				Protocol:   "TCP",
				Port:       int32(8080),
				TargetPort: intstr.FromInt(int(8080)),
			},
		},
	}

	if cr.Spec.SvcType == "Headless" {
		svcSpec.Type = "None"
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: svcObjectMeta,
		Spec:       svcSpec,
	}
}

// newServiceForPeer returns a service for FabricPeer with the same name/namespace as the cr
func newPeerVolumes(cr *fabricv1alpha1.FabricPeer) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "run",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/run",
				},
			},
		},
	}

	for key, _ := range cr.Spec.Certificate {
		volumes = append(volumes, corev1.Volume{
			Name: cr.ObjectMeta.Name + "-" + key,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.ObjectMeta.Name + "-" + key,
				},
			},
		})
	}

	return volumes
}

func newPeerVolumeMounts(cr *fabricv1alpha1.FabricPeer) []corev1.VolumeMount {
	//Basic static volume mounts
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "certificate",
			MountPath: peerMSPPath,
			SubPath:   "data/msp",
		},
		{
			Name:      "certificate",
			MountPath: peerTLSPath,
			SubPath:   "data/tls",
		},
		{
			Name:      "peerdata",
			MountPath: "/var/hyperledger/production",
			SubPath:   "data/peerdata",
		},
		// {
		// 	Name:      "run",
		// 	MountPath: "/host/var/run/",
		// },
	}

	//Add volume mounts for secrets with certificates
	for key, _ := range cr.Spec.Certificate {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      cr.ObjectMeta.Name + "-" + key,
			MountPath: peerMSPPath + key,
		})
	}

	return volumeMounts
}

func newPeerVolumeClaimTemplates(cr *fabricv1alpha1.FabricPeer) []corev1.PersistentVolumeClaim {
	return []corev1.PersistentVolumeClaim{
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PersistentVolumeClaim",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "peerdata",
				Namespace: cr.Namespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: cr.Spec.DataVolumeSize,
					},
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PersistentVolumeClaim",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "certificate",
				Namespace: cr.Namespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: cr.Spec.CertVolumeSize,
					},
				},
			},
		},
	}
}

func newPeerLabels(cr *fabricv1alpha1.FabricPeer) map[string]string {

	return map[string]string{
		"app":  cr.Kind,
		"name": cr.Name,
	}
}

func newCouchdbInit() corev1.Container {

	return corev1.Container{
		Name:  "couchdb-init",
		Image: "hyperledger/fabric-couchdb:0.4.14",
		Command: []string{
			"/bin/sh",
			"-c",
			"chown -R couchdb:couchdb /data",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "peerdata",
				MountPath: "/data",
				SubPath:   "data/couchdb",
			},
		},
	}
}
