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

package fabricorderer

import (
	"context"
	"encoding/base64"
	"reflect"

	"github.com/imdario/mergo"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/KompiTech/hyperledger-fabric-operator/pkg/config"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/KompiTech/hyperledger-fabric-operator/pkg/resources"

	crd "github.com/jiribroulik/pkg/apis/istio/v1alpha3"
	"k8s.io/apimachinery/pkg/util/intstr"

	fabricv1alpha1 "github.com/KompiTech/hyperledger-fabric-operator/pkg/apis/fabric/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ordererMSPPath = "/etc/hyperledger/orderer/msp/"
	ordererTLSPath = "/etc/hyperledger/orderer/tls/"
)

var log = logf.Log.WithName("controller_fabricorderer")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new FabricOrderer Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFabricOrderer{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("fabricorderer-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource FabricOrderer
	err = c.Watch(&source.Kind{Type: &fabricv1alpha1.FabricOrderer{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource StatefulSet and requeue the owner FabricOrderer
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &fabricv1alpha1.FabricOrderer{},
	})
	if err != nil {
		return err
	}

	// // Watch for changes to secondary resource Pods and requeue the owner FabricOrderer
	// err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
	// 	IsController: true,
	// 	OwnerType:    &appsv1.StatefulSet{},
	// })
	// if err != nil {
	// 	return err
	// }
	return nil
}

var _ reconcile.Reconciler = &ReconcileFabricOrderer{}

// ReconcileFabricOrderer reconciles a FabricOrderer object
type ReconcileFabricOrderer struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a FabricOrderer object and makes changes based on the state read
// and what is in the FabricOrderer.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileFabricOrderer) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace)
	reqLogger = reqLogger.WithName(request.Name)
	reqLogger.Info("Reconciling FabricOrderer")
	defer reqLogger.Info("Reconcile done")

	// Fetch the FabricOrderer instance
	instance := &fabricv1alpha1.FabricOrderer{}
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

	// Change state to Running when enters in Updating to prevent infinite loop
	if instance.Status.FabricOrdererState == fabricv1alpha1.StateUpdating {
		instance.Status.FabricOrdererState = fabricv1alpha1.StateRunning
		err := r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric orderer status")
			return reconcile.Result{}, err
		}
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

	//Create secrets for orderers with certificates`
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

		// Set FabricOrderer instance as the owner and controller
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

	// genesis
	secretName := instance.GetName() + "-genesis"
	data := make(map[string][]byte)
	data["genesis.block"], _ = base64.StdEncoding.DecodeString(instance.Spec.Genesis)
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

	// Set FabricOrderer instance as the owner and controller
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

	//Create sts for orderer
	// Define a new Statef object
	newSts := newOrdererStatefulSet(instance)
	pvcs := []corev1.PersistentVolumeClaim{}

	for _, item := range newOrdererVolumeClaimTemplates(instance) {
		pvc := item
		if err := controllerutil.SetControllerReference(instance, &pvc, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
		pvcs = append(pvcs, pvc)

	}
	newSts.Spec.VolumeClaimTemplates = pvcs

	// Set FabricOrderer instance as the owner and controller
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
					candidate.Spec.Template.Spec.Containers[i].Image = newSts.Spec.Template.Spec.Containers[j].Image
				}
				if !reflect.DeepEqual(current.Resources, new.Resources) {
					candidate.Spec.Template.Spec.Containers[i].Resources = newSts.Spec.Template.Spec.Containers[j].Resources
				}
			}
		}
	}

	if !reflect.DeepEqual(candidate.Spec, currentSts.Spec) {
		reqLogger.Info("UPDATING peer statefulset!!!", "Namespace", candidate.Namespace, "Name", candidate.Name)
		err = r.client.Update(context.TODO(), candidate)
		if err != nil {
			return reconcile.Result{}, err
		}
		instance.Status.FabricOrdererState = fabricv1alpha1.StateUpdating
		err := r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric orderer status")
			return reconcile.Result{}, err
		}
	} else {
		reqLogger.Info("NOTHING to update!!!")
	}

	//Create Service
	newService := newOrdererService(instance)

	// Set FabricOrderer instance as the owner and controller
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
		Servers:   resources.GetOrdererServerPorts(instance.Spec.CommonName),
	}
	newGateway := resources.NewGateway(gatewayTemplate)

	// Set FabricOrderer instance as the owner and controller
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
		Spec:      resources.GetOrdererVirtualServiceSpec(instance.GetName(), instance.Spec.CommonName),
	}
	newVirtualService := resources.NewVirtualService(vsvcTemplate)

	// Set FabricOrderer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newVirtualService, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Virtual service already exists
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

	for ok := true; ok; ok = instance.Status.FabricOrdererState == fabricv1alpha1.StateUpdating && pod.Status.Phase == "Running" {
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Name + "-0", Namespace: instance.Namespace}, pod)
		if err != nil {
			if instance.Spec.Replicas != int32(0) {
				reqLogger.Error(err, "failed to get pods", "Namespace", instance.Namespace, "Name", instance.Name)
				return reconcile.Result{}, err
			}
			ordererState := fabricv1alpha1.StateSuspended
			reqLogger.Info("Update orderer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", ordererState)
			instance.Status.FabricOrdererState = ordererState
			err := r.client.Update(context.TODO(), instance)
			if err != nil {
				reqLogger.Error(err, "failed to update orderer status")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
	}

	ordererState := ""

	if pod.Status.Phase != "Running" {
		ordererState = fabricv1alpha1.StateCreating
	} else {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "orderer" {
				if status.State.Running != nil {
					ordererState = "Running"
				} else if status.RestartCount > 0 {
					ordererState = "Error"
				}
			}
		}
	}

	// Update status.Nodes if needed
	if !reflect.DeepEqual(ordererState, instance.Status.FabricOrdererState) {
		reqLogger.Info("Update fabricorderer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", ordererState)
		instance.Status.FabricOrdererState = ordererState
		err := r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric orderer status")
			return reconcile.Result{}, err
		}
	}

	// sts already exists - don't requeue
	reqLogger.Info("Skip reconcile: sts already exists", "sts.Namespace", currentSts.Namespace, "sts.Name", currentSts.Name)
	return reconcile.Result{}, nil
}

func newOrdererStatefulSet(cr *fabricv1alpha1.FabricOrderer) *appsv1.StatefulSet {
	replicas := cr.Spec.Replicas

	labels := newOrdererLabels(cr)

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Name,
			Namespace:       cr.Namespace,
			Labels:          labels,
			OwnerReferences: cr.OwnerReferences,
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
					InitContainers: resources.GetInitContainer(resources.VaultInit{
						Organization: cr.Spec.Organization,
						CommonName:   cr.Spec.CommonName,
						VaultAddress: config.VaultAddress,
						TLSPath:      ordererTLSPath,
						MSPPath:      ordererMSPPath,
						Cluster:      cr.GetAnnotations()["region"],
						NodeType:     "orderer",
					}), //TODO
					Containers: newOrdererContainers(cr),
					Volumes:    newOrdererVolumes(cr),
				},
			},

			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			//Volume Claims templates
			//VolumeClaimTemplates: newOrdererVolumeClaimTemplates(cr),
		},
	}
}

func newOrdererContainers(cr *fabricv1alpha1.FabricOrderer) []corev1.Container {
	privileged := true
	procMount := corev1.DefaultProcMount

	metricsImage := cr.Spec.MetricsImage
	if metricsImage == "" {
		metricsImage = "eu.gcr.io/hl-development-212213/fabric-node-metrics:latest"
	}

	baseContainers := []corev1.Container{
		{
			Name:            "orderer",
			Image:           cr.Spec.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			//workingDir
			WorkingDir: "/opt/gopath/src/github.com/hyperledger/fabric/orderer",
			Ports: []corev1.ContainerPort{
				{
					Name:          "containerport1",
					ContainerPort: int32(7050),
				},
				{
					Name:          "containerport2",
					ContainerPort: int32(8080),
				},
			},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("500Mi"),
					corev1.ResourceCPU:    resource.MustParse("300m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("500Mi"),
					corev1.ResourceCPU:    resource.MustParse("300m"),
				},
			},
			//command
			Command: []string{
				"/bin/sh",
			},
			//args
			Args: []string{
				"-c",
				"orderer",
				//"sleep 999999999999",
			},

			// LivenessProbe: &v1.Probe{
			// 	Handler: v1.Handler{
			// 		HTTPGet: &v1.HTTPGetAction{
			// 			Path: "/health",
			// 			Port: intstr.FromString(httpPortName),
			// 		},
			// 	},
			// 	InitialDelaySeconds: int32(30),
			// 	PeriodSeconds:       int32(5),
			// },
			// ReadinessProbe: &corev1.Probe{
			// 	Handler: corev1.Handler{
			// 		HTTPGet: &corev1.HTTPGetAction{
			// 			Path: "/health?ready=1",
			// 			Port: intstr.FromInt(7050),
			// 		},
			// 	},
			// 	InitialDelaySeconds: int32(10),
			// 	PeriodSeconds:       int32(5),
			// 	FailureThreshold:    int32(25),
			// },

			//Volume mount
			VolumeMounts:             newOrdererVolumeMounts(cr),
			TerminationMessagePath:   "/dev/termination-log",
			TerminationMessagePolicy: "File",
			//ENV
			Env: newOrdererContainerEnv(cr),
		},
		{
			Name:            "metrics",
			Image:           metricsImage,
			ImagePullPolicy: corev1.PullAlways,
			SecurityContext: &corev1.SecurityContext{
				Privileged: &privileged,
				ProcMount:  &procMount,
			},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("50Mi"),
					corev1.ResourceCPU:    resource.MustParse("50m"),
				},
			},
			Ports: []corev1.ContainerPort{
				{
					Name:          "metrics",
					Protocol:      "TCP",
					ContainerPort: int32(9141),
				},
			},
			TerminationMessagePath:   "/dev/termination-log",
			TerminationMessagePolicy: "File",
			Env: []corev1.EnvVar{
				{
					Name:  "NODE_NAME",
					Value: cr.Name,
				},
				{
					Name:  "NODE_TYPE",
					Value: "orderer",
				},
				{
					Name:  "MSP_DIR",
					Value: "/etc/hyperledger/orderer/msp",
				},
				{
					Name:  "TLS_DIR",
					Value: "/etc/hyperledger/orderer/tls",
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "certificate",
					MountPath: "/etc/hyperledger/orderer/msp",
					SubPath:   "data/msp",
				},
				{
					Name:      "certificate",
					MountPath: "/etc/hyperledger/orderer/tls",
					SubPath:   "data/tls",
				},
			},
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

func newOrdererContainerEnv(cr *fabricv1alpha1.FabricOrderer) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{
			Name:  "ORDERER_METRICS_PROVIDER",
			Value: "prometheus",
		},
		{
			Name:  "ORDERER_METRICS_PROMETHEUS_HANDLERPATH",
			Value: "/metrics",
		},
		{
			Name:  "ORDERER_OPERATIONS_LISTENADDRESS",
			Value: "0.0.0.0:8080",
		},
		{
			Name:  "ORDERER_GENERAL_LOGLEVEL",
			Value: "info",
		},
		{
			Name:  "ORDERER_HOST",
			Value: cr.Spec.CommonName,
		},
		{
			Name:  "ORDERER_GENERAL_LISTENADDRESS",
			Value: "0.0.0.0",
		},
		{
			Name:  "ORDERER_GENERAL_GENESISMETHOD",
			Value: "file",
		},
		{
			Name:  "ORDERER_GENERAL_TLS_ENABLED",
			Value: "true",
		},
		{
			Name:  "ORDERER_GENERAL_LOCALMSPID",
			Value: cr.Spec.MspId,
		},
		{
			Name:  "ORDERER_GENERAL_LOCALMSPDIR",
			Value: "/etc/hyperledger/orderer/msp",
		},
		{
			Name:  "ORDERER_GENERAL_GENESISFILE",
			Value: "/etc/hyperledger/orderer/orderer.genesis.block/genesis.block",
		},
		{
			Name:  "ORDERER_GENERAL_TLS_PRIVATEKEY",
			Value: "/etc/hyperledger/orderer/tls/cert.key",
		},
		{
			Name:  "ORDERER_GENERAL_TLS_CERTIFICATE",
			Value: "/etc/hyperledger/orderer/tls/cert.crt",
		},
		{
			Name:  "ORDERER_GENERAL_TLS_ROOTCAS",
			Value: "/etc/hyperledger/orderer/tls/ca.crt",
		},
		{
			Name:  "ORDERER_GENERAL_TLS_CLIENTAUTHREQUIRED",
			Value: "false",
		},
		{
			Name:  "ORDERER_GENERAL_TLS_CLIENTROOTCAS",
			Value: "/etc/hyperledger/orderer/tls/ca.crt",
		},
		{
			Name:  "FABRIC_CA_CLIENT_TLS_CERTFILES",
			Value: "/etc/hyperledger/orderer/tls/ca.crt",
		},
		{
			Name:  "ORG_ADMIN_CERT",
			Value: "/etc/hyperledger/orderer/msp/admincerts/cert.pem",
		},
		// {
		// 	Name:  "ORDERER_KAFKA_TLS_ENABLED",
		// 	Value: "true",
		// },
		// {
		// 	Name:  "ORDERER_KAFKA_TLS_PRIVATEKEY_FILE",
		// 	Value: "/etc/hyperledger/orderer/tls/kafka_cert.key",
		// },
		// {
		// 	Name:  "ORDERER_KAFKA_TLS_CERTIFICATE_FILE",
		// 	Value: "/etc/hyperledger/orderer/tls/kafka_cert.crt",
		// },
		// {
		// 	Name:  "ORDERER_KAFKA_TLS_ROOTCAS_FILE",
		// 	Value: "/etc/hyperledger/orderer/tls/kafka_ca.crt",
		// },
		{
			Name:  "ORDERER_GENERAL_CLUSTER_ROOTCAS",
			Value: "/etc/hyperledger/orderer/tls/ca.crt",
		},
		{
			Name:  "ORDERER_GENERAL_CLUSTER_CLIENTPRIVATEKEY",
			Value: "/etc/hyperledger/orderer/tls/cert.key",
		},
		{
			Name:  "ORDERER_GENERAL_CLUSTER_CLIENTCERTIFICATE",
			Value: "/etc/hyperledger/orderer/tls/cert.crt",
		},
		{
			Name:  "GODEBUG",
			Value: "netdns=go",
		},
	}
	return env
}

func newOrdererService(cr *fabricv1alpha1.FabricOrderer) *corev1.Service {
	annotations := make(map[string]string)

	annotations["fqdn"] = cr.Spec.CommonName
	annotations["prometheus.io/scrape"] = "true"
	annotations["prometheus.io/port"] = "8080"

	var svcObjectMeta metav1.ObjectMeta
	var svcSpec corev1.ServiceSpec
	svcObjectMeta = metav1.ObjectMeta{
		Name:        cr.GetName(),
		Namespace:   cr.GetNamespace(),
		Labels:      newOrdererLabels(cr),
		Annotations: annotations,
	}

	svcSpec = corev1.ServiceSpec{
		Selector: newOrdererLabels(cr),
		Type:     cr.Spec.SvcType,
		Ports: []corev1.ServicePort{
			{
				Name:       "grpc-orderer",
				Protocol:   "TCP",
				Port:       int32(7050),
				TargetPort: intstr.FromInt(int(7050)),
			},
			{
				Name:       "metrics",
				Protocol:   "TCP",
				Port:       int32(8080),
				TargetPort: intstr.FromInt(int(8080)),
			},
			{
				Name:       "certmetrics",
				Protocol:   "TCP",
				Port:       int32(9141),
				TargetPort: intstr.FromInt(int(9141)),
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

func newOrdererVolumes(cr *fabricv1alpha1.FabricOrderer) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: newGenesisSecretName(cr.GetName()),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: newGenesisSecretName(cr.GetName()),
				},
			},
		},
	}

	if cr.Spec.NodeOUsEnabled {
		volumes = append(volumes, corev1.Volume{
			Name: "node-ous",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "node-ous",
					},
				},
			},
		})
	}

	for key := range cr.Spec.Certificate {
		volumes = append(volumes, corev1.Volume{
			Name: cr.GetName() + "-" + key,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.GetName() + "-" + key,
				},
			},
		})
	}

	return volumes

}

func newOrdererVolumeMounts(cr *fabricv1alpha1.FabricOrderer) []corev1.VolumeMount {

	//Basic static volume mounts
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "certificate",
			MountPath: "/etc/hyperledger/orderer/msp",
			SubPath:   "data/msp",
		},
		{
			Name:      "certificate",
			MountPath: "/etc/hyperledger/orderer/tls",
			SubPath:   "data/tls",
		},
		{
			Name:      newGenesisSecretName(cr.GetName()),
			MountPath: "/etc/hyperledger/orderer/orderer.genesis.block",
		},
		{
			Name:      "ordererdata",
			MountPath: "/var/hyperledger/production",
			SubPath:   "data/ordererdata",
		},
	}

	if cr.Spec.NodeOUsEnabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "node-ous",
			MountPath: "/etc/hyperledger/orderer/msp/config.yaml",
			SubPath:   "config.yaml",
		})
	}

	//Add volume mounts for secrets with certificates
	for key := range cr.Spec.Certificate {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      cr.ObjectMeta.Name + "-" + key,
			MountPath: ordererMSPPath + key,
		})
	}

	return volumeMounts
}

func newOrdererVolumeClaimTemplates(cr *fabricv1alpha1.FabricOrderer) []corev1.PersistentVolumeClaim {
	return []corev1.PersistentVolumeClaim{
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PersistentVolumeClaim",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ordererdata",
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

func newOrdererLabels(cr *fabricv1alpha1.FabricOrderer) map[string]string {

	return map[string]string{
		"app":  cr.Kind,
		"name": cr.Name,
	}
}

func newGenesisSecretName(name string) string {
	return name + "-genesis"
}
