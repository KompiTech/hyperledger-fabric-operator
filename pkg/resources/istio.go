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

package resources

import (
	crd "github.com/jiribroulik/pkg/apis/istio/v1alpha3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	extPort     = 7051
	ccPort      = 7052
	eventPort   = 7053
	ordererPort = 7050
)

type GatewayTemplate struct {
	Name            string
	Namespace       string
	Servers         *[]crd.Server
	Label           map[string]string
	OwnerReferences []metav1.OwnerReference
}

type VirtualServiceTemplate struct {
	Name            string
	Namespace       string
	Spec            *crd.VirtualServiceSpec
	Label           map[string]string
	OwnerReferences []metav1.OwnerReference
}

func NewVirtualService(vsvc VirtualServiceTemplate) *crd.VirtualService {

	vsvcObjectMeta := metav1.ObjectMeta{
		Name:            vsvc.Name,
		Namespace:       vsvc.Namespace,
		Labels:          vsvc.Label,
		OwnerReferences: vsvc.OwnerReferences,
	}
	return &crd.VirtualService{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VirtualService",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: vsvcObjectMeta,
		Spec:       *vsvc.Spec,
	}
}

func NewGateway(gtw GatewayTemplate) *crd.Gateway {

	gtwObjectMeta := metav1.ObjectMeta{
		Name:            gtw.Name,
		Namespace:       gtw.Namespace,
		Labels:          gtw.Label,
		OwnerReferences: gtw.OwnerReferences,
	}
	selector := map[string]string{"istio": "ingressgateway"}
	gtwSpec := crd.GatewaySpec{Selector: selector, Servers: *gtw.Servers}

	return &crd.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: gtwObjectMeta,
		Spec:       gtwSpec,
	}
}

func GetPeerVirtualServiceSpec(name, target string) *crd.VirtualServiceSpec {
	spec := crd.VirtualServiceSpec{
		Hosts:    []string{target},
		Gateways: []string{name},
	}

	dst1 := crd.Destination{Port: crd.PortSelector{Number: uint32(extPort)}, Host: name}
	tcpMatchreq1 := []crd.TlsL4MatchAttributes{crd.TlsL4MatchAttributes{Port: extPort, Sni_hosts: []string{target}}}
	dstWeight1 := []crd.DestinationWeight{crd.DestinationWeight{Destination: dst1, Weight: 100}}
	tcpRoute1 := crd.TLSRoute{Match: tcpMatchreq1, Route: dstWeight1}

	dst2 := crd.Destination{Port: crd.PortSelector{Number: uint32(ccPort)}, Host: name}
	tcpMatchreq2 := []crd.TlsL4MatchAttributes{crd.TlsL4MatchAttributes{Port: ccPort, Sni_hosts: []string{target}}}
	dstWeight2 := []crd.DestinationWeight{crd.DestinationWeight{Destination: dst2, Weight: 100}}
	tcpRoute2 := crd.TLSRoute{Match: tcpMatchreq2, Route: dstWeight2}

	spec.Tls = []crd.TLSRoute{tcpRoute1, tcpRoute2}
	return &spec
}

func GetOrdererVirtualServiceSpec(name, target string) *crd.VirtualServiceSpec {
	spec := crd.VirtualServiceSpec{
		Hosts:    []string{target},
		Gateways: []string{name},
	}

	dst := crd.Destination{Port: crd.PortSelector{Number: uint32(ordererPort)}, Host: name}
	tcpMatchreq := []crd.TlsL4MatchAttributes{crd.TlsL4MatchAttributes{Port: ordererPort, Sni_hosts: []string{target}}}
	dstWeight := []crd.DestinationWeight{crd.DestinationWeight{Destination: dst, Weight: 100}}
	tcpRoute := crd.TLSRoute{Match: tcpMatchreq, Route: dstWeight}

	spec.Tls = []crd.TLSRoute{tcpRoute}
	return &spec
}

func GetPeerServerPorts(target string) *[]crd.Server {
	server1 := crd.Server{Port: crd.Port{Name: "https-ext-listen-endpoint", Protocol: crd.ProtocolHTTPS, Number: extPort}, Hosts: []string{target}, TLS: &crd.TLSOptions{Mode: crd.TLSModePassThrough}}
	server2 := crd.Server{Port: crd.Port{Name: "https-chaincode-listen", Protocol: crd.ProtocolHTTPS, Number: ccPort}, Hosts: []string{target}, TLS: &crd.TLSOptions{Mode: crd.TLSModePassThrough}}
	return &[]crd.Server{server1, server2}
}

func GetOrdererServerPorts(target string) *[]crd.Server {
	server := crd.Server{Port: crd.Port{Name: "https-orderer", Protocol: crd.ProtocolHTTPS, Number: ordererPort}, Hosts: []string{target}, TLS: &crd.TLSOptions{Mode: crd.TLSModePassThrough}}
	return &[]crd.Server{server}
}
