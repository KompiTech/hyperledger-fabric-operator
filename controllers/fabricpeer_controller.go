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
	"reflect"

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

const (
	peerMSPPath = "/etc/hyperledger/fabric/msp/"
	peerTLSPath = "/etc/hyperledger/fabric/tls/"
)

// FabricPeerReconciler reconciles a FabricPeer object
type FabricPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=fabric.kompitech.com,resources=fabricpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fabric.kompitech.com,resources=fabricpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fabric.kompitech.com,resources=fabricpeers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FabricPeer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *FabricPeerReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	reqLogger := log.Log.WithValues("Request.Namespace", request.Namespace)
	reqLogger = reqLogger.WithName(request.Name)
	reqLogger.Info("Reconciling FabricPeer")
	defer reqLogger.Info("Reconcile done")

	// Fetch the FabricPeer instance
	instance := &fabricv1alpha1.FabricPeer{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
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
	if instance.Status.FabricPeerState == fabricv1alpha1.StateUpdating {
		instance.Status.FabricPeerState = fabricv1alpha1.StateRunning
		err := r.Client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric peer status")
			return ctrl.Result{}, err
		}
	}

	//Set global namespace
	namespace := instance.GetNamespace()

	//Create namespace
	newServiceAccount := resources.NewServiceAccount("vault", namespace)
	currentServiceAccount := &corev1.ServiceAccount{}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: "vault", Namespace: namespace}, currentServiceAccount)
	reqLogger.Error(err, "Error reading SA", "ISNOTFOUND", errors.IsNotFound(err))
	if err != nil && errors.IsNotFound(err) {
		//Secret not exists
		reqLogger.Info("Creating a new service account", "Namespace", newServiceAccount.GetNamespace(), "Name", newServiceAccount.GetName())
		err = r.Client.Create(context.TODO(), newServiceAccount)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	//Create secrets for peers with certificates`
	newCertSecret := newCertificateSecret(instance.Name+"-cacerts", namespace, instance.Spec.Certificate)
	newTLSCertSecret := newCertificateSecret(instance.Name+"-tlscacerts", namespace, instance.Spec.TLSCertificate)
	newCertSecrets := []*corev1.Secret{newCertSecret, newTLSCertSecret}

	for _, newSecret := range newCertSecrets {
		// Set FabricPeer instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, newSecret, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		currentSecret := &corev1.Secret{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: newSecret.Name, Namespace: newSecret.Namespace}, currentSecret)
		if err != nil && errors.IsNotFound(err) {
			//Secret not exists
			reqLogger.Info("Creating a new secret", "Namespace", newSecret.Namespace, "Name", newSecret.Name)
			err = r.Client.Create(context.TODO(), newSecret)
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
			err = r.Client.Update(context.TODO(), newSecret)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

	}

	//Create sts for peer
	// Define a new Statef object
	newSts := newPeerStatefulSet(instance)
	pvcs := []corev1.PersistentVolumeClaim{}

	for _, item := range newPeerVolumeClaimTemplates(instance) {
		pvc := item
		if err := controllerutil.SetControllerReference(instance, &pvc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		pvcs = append(pvcs, pvc)

	}
	newSts.Spec.VolumeClaimTemplates = pvcs

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newSts, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this StatefulSet already exists
	currentSts := &appsv1.StatefulSet{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new StatefulSet", "Namespace", newSts.Namespace, "Name", newSts.Name)
		err = r.Client.Create(context.TODO(), newSts)
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
		err = r.Client.Update(context.TODO(), candidate)
		if err != nil {
			return ctrl.Result{}, err
		}
		instance.Status.FabricPeerState = fabricv1alpha1.StateUpdating
		err := r.Client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric peer status")
			return ctrl.Result{}, err
		}
	} else {
		reqLogger.Info("NOTHING to update!!!")
	}

	//Create Service
	newService := newPeerService(instance)

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newService, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Service already exists
	currentService := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", newService.Namespace, "Name", newService.Name)
		err = r.Client.Create(context.TODO(), newService)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
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
		Servers:   resources.GetPeerServerPorts(instance.Spec.CommonName),
	}
	newGateway := resources.NewGateway(gatewayTemplate)

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newGateway, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Gateway already exists
	currentGateway := &crd.Gateway{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: newGateway.Name, Namespace: newGateway.Namespace}, currentGateway)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Gateway", "Namespace", newGateway.Namespace, "Name", newGateway.Name)
		err = r.Client.Create(context.TODO(), newGateway)
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
		Spec:      resources.GetPeerVirtualServiceSpec(instance.GetName(), instance.Spec.CommonName),
	}
	newVirtualService := resources.NewVirtualService(vsvcTemplate)

	// Set FabricPeer instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newVirtualService, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Gateway already exists
	currentVirtualService := &crd.VirtualService{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: newVirtualService.Name, Namespace: newVirtualService.Namespace}, currentVirtualService)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Istio Virtual Service", "Namespace", newVirtualService.Namespace, "Name", newVirtualService.Name)
		err = r.Client.Create(context.TODO(), newVirtualService)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	//Update CR status
	pod := &corev1.Pod{}

	for ok := true; ok; ok = instance.Status.FabricPeerState == fabricv1alpha1.StateUpdating && pod.Status.Phase == "Running" {
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: instance.Name + "-0", Namespace: instance.Namespace}, pod)
		if err != nil {
			if instance.Spec.Replicas != int32(0) {
				reqLogger.Error(err, "failed to get pods", "Namespace", instance.Namespace, "Name", instance.Name)
				return ctrl.Result{}, err
			}
			peerState := fabricv1alpha1.StateSuspended
			reqLogger.Info("Update fabric peer status", "Namespace", instance.Namespace, "Name", instance.Name, "State", peerState)
			instance.Status.FabricPeerState = peerState
			err := r.Client.Update(context.TODO(), instance)
			if err != nil {
				reqLogger.Error(err, "failed to update fabric peer status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
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
		err := r.Client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update Fabric peer status")
			return ctrl.Result{}, err
		}
	}

	// Pod already exists - don't requeue
	reqLogger.Info("Skip reconcile: sts already exists", "Namespace", instance.Namespace, "Name", instance.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FabricPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fabricv1alpha1.FabricPeer{}).
		Complete(r)
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

	couchDBContainer := corev1.Container{
		Name:            "couchdb",
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
	}

	couchDBImage := "hyperledger/fabric-couchdb:0.4.14"
	if cr.Spec.CouchDBImage != "" {
		couchDBImage = cr.Spec.CouchDBImage
		couchDBContainer.Env = []corev1.EnvVar{
			{
				Name:  "COUCHDB_USER",
				Value: "admin",
			},
			{
				Name:  "COUCHDB_PASSWORD",
				Value: "password",
			},
		}
	}

	couchDBContainer.Image = couchDBImage

	dindImage := "docker:18.09.3-dind"
	if cr.Spec.DINDImage != "" {
		dindImage = cr.Spec.DINDImage
	}

	metricsImage := cr.Spec.MetricsImage
	if metricsImage == "" {
		metricsImage = "eu.gcr.io/hl-development-212213/fabric-node-metrics:latest"
	}

	baseContainers := []corev1.Container{
		{
			Name:            "peer",
			Image:           cr.Spec.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			WorkingDir:      "/opt/gopath/src/github.com/hyperledger/fabric/peer",
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
			Command: []string{
				"/bin/sh",
			},
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
			VolumeMounts:             newPeerVolumeMounts(cr),
			TerminationMessagePath:   "/dev/termination-log",
			TerminationMessagePolicy: "File",
			Env:                      newPeerContainerEnv(cr),
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
			},
			VolumeMounts: []corev1.VolumeMount{
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
			},
		},
		{
			Name:            "dind",
			Image:           dindImage,
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
		couchDBContainer,
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
	builderImage := "smolaon/fabric-ccenv:amd64-2.0.0-snapshot-e77813c85"
	if cr.Spec.BuilderImage != "" {
		builderImage = cr.Spec.BuilderImage
	}
	runtimeImage := "smolaon/fabric-baseos:amd64-2.0.0-snapshot-e77813c85"
	if cr.Spec.RuntimeImage != "" {
		runtimeImage = cr.Spec.RuntimeImage
	}

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
			Name:  "FABRIC_LOGGING_SPEC",
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
			Value: builderImage,
		},
		{
			Name:  "CORE_CHAINCODE_GOLANG_RUNTIME",
			Value: runtimeImage,
		},
	}
	if cr.Spec.BootstrapNodeAddress != "" {
		env = append(env, corev1.EnvVar{
			Name:  "CORE_PEER_GOSSIP_BOOTSTRAP",
			Value: cr.Spec.BootstrapNodeAddress,
		})
	}

	if cr.Spec.CouchDBImage != "" {
		additionalEnv := []corev1.EnvVar{
			{
				Name:  "CORE_LEDGER_STATE_COUCHDBCONFIG_USERNAME",
				Value: "admin",
			},
			{
				Name:  "CORE_LEDGER_STATE_COUCHDBCONFIG_PASSWORD",
				Value: "password",
			},
		}
		env = append(env, additionalEnv...)
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

	if cr.Spec.NodeOUsEnabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "node-ous",
			MountPath: "/etc/hyperledger/fabric/msp/config.yaml",
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
