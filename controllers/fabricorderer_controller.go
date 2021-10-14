/*
Copyright 2021.

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

package controllers

import (
	"context"
	"encoding/base64"
	"reflect"
	"time"

	crd "github.com/jiribroulik/pkg/apis/istio/v1alpha3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fabricv1alpha1 "github.com/KompiTech/hyperledger-fabric-operator/api/v1alpha1"
	"github.com/KompiTech/hyperledger-fabric-operator/pkg/config"
	"github.com/KompiTech/hyperledger-fabric-operator/pkg/resources"
	"github.com/imdario/mergo"
)

// FabricOrdererReconciler reconciles a FabricOrderer object
type FabricOrdererReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	ordererMSPPath = "/etc/hyperledger/orderer/msp/"
	ordererTLSPath = "/etc/hyperledger/orderer/tls/"
)

//+kubebuilder:rbac:groups=fabric.kompitech.com,resources=fabricorderers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fabric.kompitech.com,resources=fabricorderers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fabric.kompitech.com,resources=fabricorderers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FabricOrderer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *FabricOrdererReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	reqLogger := log.Log.WithValues("namespace", request.Namespace)
	reqLogger = reqLogger.WithName(request.Name)
	reqLogger.Info("Reconciling FabricOrderer")
	defer reqLogger.Info("Reconcile done")

	// Fetch the FabricOrderer instance
	instance := &fabricv1alpha1.FabricOrderer{}
	err := r.Client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Change state to Running when enters in Updating to prevent infinite loop
	if instance.State == fabricv1alpha1.StateUpdating {
		instance.State = fabricv1alpha1.StateRunning
		err := r.Client.Update(ctx, instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric orderer status")
			return ctrl.Result{}, err
		}
	}

	//Set global namespace
	namespace := instance.GetNamespace()

	//Create namespace
	newServiceAccount := resources.NewServiceAccount("vault", namespace)
	currentServiceAccount := &corev1.ServiceAccount{}

	err = r.Client.Get(ctx, types.NamespacedName{Name: "vault", Namespace: namespace}, currentServiceAccount)
	if err != nil && errors.IsNotFound(err) {
		//Secret not exists
		reqLogger.Info("Creating a new service account", "Namespace", newServiceAccount.GetNamespace(), "Name", newServiceAccount.GetName())
		err = r.Client.Create(ctx, newServiceAccount)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	//Create secrets for orderers with certificates`
	// for key, secretData := range instance.Spec.Certificate {
	newCertSecret := newCertificateSecret(instance.Name+"-cacerts", namespace, instance.Spec.Certificate)
	newTLSCertSecret := newCertificateSecret(instance.Name+"-tlscacerts", namespace, instance.Spec.TLSCertificate)
	newCertSecrets := []*corev1.Secret{newCertSecret, newTLSCertSecret}

	for _, newSecret := range newCertSecrets {
		// Set FabricOrderer instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, newSecret, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		currentSecret := &corev1.Secret{}
		err = r.Client.Get(ctx, types.NamespacedName{Name: newSecret.Name, Namespace: newSecret.Namespace}, currentSecret)
		if err != nil && errors.IsNotFound(err) {
			//Secret not exists
			reqLogger.Info("Creating a new secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
			err = r.Client.Create(ctx, newSecret)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else if err != nil {
			return ctrl.Result{}, err
		}

		// Updating secrets
		eq := reflect.DeepEqual(newSecret.Data, currentSecret.Data)
		if !eq {
			reqLogger.Info("Updating secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
			err = r.Client.Update(ctx, newSecret)
			if err != nil {
				return ctrl.Result{}, err
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
	if err := controllerutil.SetControllerReference(instance, newSecret, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	currentSecret := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: newSecret.Name, Namespace: newSecret.Namespace}, currentSecret)
	if err != nil && errors.IsNotFound(err) {
		//Secret not exists
		reqLogger.Info("Creating a new secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
		err = r.Client.Create(ctx, newSecret)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Updating secrets
	eq := reflect.DeepEqual(newSecret.Data, currentSecret.Data)
	if !eq {
		reqLogger.Info("Updating secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
		err = r.Client.Update(ctx, newSecret)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	//Create sts for orderer
	// Define a new Statef object
	newSts := newOrdererStatefulSet(instance)
	pvcs := []corev1.PersistentVolumeClaim{}

	for _, item := range newOrdererVolumeClaimTemplates(instance) {
		pvc := item
		if err := controllerutil.SetControllerReference(instance, &pvc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		pvcs = append(pvcs, pvc)

	}
	newSts.Spec.VolumeClaimTemplates = pvcs

	// Set FabricOrderer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newSts, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this StatefulSet already exists
	currentSts := &appsv1.StatefulSet{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new StatefulSet", "Namespace", newSts.Namespace, "Name", newSts.Name)
		err = r.Client.Create(ctx, newSts)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
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
		err = r.Client.Update(ctx, candidate)
		if err != nil {
			return ctrl.Result{}, err
		}
		instance.State = fabricv1alpha1.StateUpdating
		err := r.Client.Update(ctx, instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric orderer status")
			return ctrl.Result{}, err
		}
	} else {
		reqLogger.Info("NOTHING to update!!!")
	}

	//Create Service
	newService := newOrdererService(instance)

	// Set FabricOrderer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newService, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Service already exists
	currentService := &corev1.Service{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", newService.Namespace, "Name", newService.Name)
		err = r.Client.Create(ctx, newService)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	err = r.Client.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = resources.CheckDNS(currentService.Spec.ClusterIP, currentService.GetObjectMeta().GetAnnotations()["fqdn"])
	if err != nil {
		reqLogger.Error(err, "failed check/update dns", "Namespace", instance.Namespace, "Name", instance.Name, "ServiceIP", currentService.Spec.ClusterIP, "CurrentFQDN", currentService.GetObjectMeta().GetAnnotations()["fqdn"])
		return ctrl.Result{}, err
	}

	//Create Gateway
	gatewayTemplate := resources.GatewayTemplate{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
		Servers:   resources.GetOrdererServerPorts(instance.Spec.CommonName),
	}
	newGateway := resources.NewGateway(gatewayTemplate)

	// Set FabricOrderer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newGateway, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Gateway already exists
	currentGateway := &crd.Gateway{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: newGateway.Name, Namespace: newGateway.Namespace}, currentGateway)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Gateway", "Namespace", newGateway.Namespace, "Name", newGateway.Name)
		err = r.Client.Create(ctx, newGateway)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	//Crate Virtual Service
	vsvcTemplate := resources.VirtualServiceTemplate{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
		Spec:      resources.GetOrdererVirtualServiceSpec(instance.GetName(), instance.Spec.CommonName),
	}
	newVirtualService := resources.NewVirtualService(vsvcTemplate)

	// Set FabricOrderer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newVirtualService, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Virtual service already exists
	currentVirtualService := &crd.VirtualService{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: newVirtualService.Name, Namespace: newVirtualService.Namespace}, currentVirtualService)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Istio Virtual Service", "Namespace", newVirtualService.Namespace, "Name", newVirtualService.Name)
		err = r.Client.Create(ctx, newVirtualService)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	//Update CR status
	pod := &corev1.Pod{}

	for ok := true; ok; ok = instance.State == fabricv1alpha1.StateUpdating && pod.Status.Phase == "Running" {
		err = r.Client.Get(ctx, types.NamespacedName{Name: instance.Name + "-0", Namespace: instance.Namespace}, pod)
		if err != nil {
			if instance.Spec.Replicas != int32(0) {
				reqLogger.Error(err, "failed to get pods", "Namespace", instance.Namespace, "Name", instance.Name)
				return ctrl.Result{}, err
			}
			ordererState := fabricv1alpha1.StateSuspended
			reqLogger.Info("Update orderer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", ordererState)
			instance.State = ordererState
			err := r.Client.Update(ctx, instance)
			if err != nil {
				reqLogger.Error(err, "failed to update orderer status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
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
	if ordererState != instance.Status.State {
		reqLogger.Info("Update fabricorderer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", ordererState)
		instance.State = ordererState
		err := r.Status().Update(ctx, instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric orderer status")
			return ctrl.Result{}, err
		}
	}

	podNames := []string{instance.Name + "-0"}

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, instance.Status.Nodes) {
		instance.Status.Nodes = podNames
		err := r.Status().Update(ctx, instance)
		if err != nil {
			reqLogger.Error(err, "Failed to update FabricOrderer status")
			return ctrl.Result{}, err
		}
	}

	// sts already exists - don't requeue
	reqLogger.Info("Skip reconcile: sts already exists", "sts.Namespace", currentSts.Namespace, "sts.Name", currentSts.Name)

	if ordererState == fabricv1alpha1.StateRunning {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FabricOrdererReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fabricv1alpha1.FabricOrderer{}).
		Complete(r)
}

func newCertificateSecret(name, namespace string, certs []fabricv1alpha1.CertificateSecret) *corev1.Secret {
	data := make(map[string][]byte)
	for _, item := range certs {
		data[name] = []byte(item.Value)
	}
	newSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
	return newSecret
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
		metricsImage = "kompitech/fabric-node-metrics:latest"
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

	volumes = append(volumes, corev1.Volume{
		Name: cr.GetName() + "-cacerts",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: cr.GetName() + "-cacerts",
			},
		},
	})

	volumes = append(volumes, corev1.Volume{
		Name: cr.GetName() + "-tlscacerts",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: cr.GetName() + "-tlscacerts",
			},
		},
	})

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
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      cr.ObjectMeta.Name + "-cacerts",
		MountPath: ordererMSPPath + "cacerts",
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      cr.ObjectMeta.Name + "-tlscacerts",
		MountPath: ordererMSPPath + "tlscacerts",
	})

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
