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
	corev1 "k8s.io/api/core/v1"
)

type VaultInit struct {
	Organization string
	CommonName   string
	VaultAddress string
	TLSPath      string
	MSPPath      string
	Cluster      string
	NodeType     string
}

func GetInitContainer(vault VaultInit) []corev1.Container {

	// VaultAddress := "https://10.27.247.45:8200"
	// TLSPath := "/etc/hyperledger/fabric/tls"
	// MSPPath := "/etc/hyperledger/fabric/msp"

	cmd := `if [ ! -f "$MSP_PATH"/keystore/peer.key ]; then
	export KUBE_TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token);
	mkdir "$MSP_PATH"/signcerts;
	mkdir "$MSP_PATH"/keystore;
	mkdir "$MSP_PATH"/cacerts;
	mkdir /etc/vault;
	curl --request POST -k --data '{"jwt": "'"$KUBE_TOKEN"'", "role": "hyperledger"}' -k "$VAULT_ADDRESS"/v1/auth/kubernetes-"$REGION_NAME"/login | jq -j '.auth.client_token' > /etc/vault/token;
	export X_VAULT_TOKEN=$(cat /etc/vault/token);
	curl -XPOST -k -H "X-Vault-Token: $X_VAULT_TOKEN" -d '{"common_name": "'$COMMON_NAME'"}' "$VAULT_ADDRESS"/v1/"$ORG_ID"/issue/MSP > /tmp/response;
	cat /tmp/response | jq -r -j .data.certificate > "$MSP_PATH"/signcerts/peer.crt;
	cat /tmp/response | jq -r -j .data.private_key > "$MSP_PATH"/keystore/peer.key;
	cat /tmp/response | jq -r -j .data.issuing_ca > "$MSP_PATH"/cacerts/ca.crt;
	fi;`

	if vault.NodeType == "orderer" {
		// s := `if [ ! -f "$TLS_PATH"/kafka_cert.key ]; then
		// curl -XGET -k -H "X-Vault-Token: $X_VAULT_TOKEN" "$VAULT_ADDRESS"/v1/secret/kafka-cluster-int-kv > /tmp/tlsresponse;
		// cat /tmp/tlsresponse | jq -r -j .data.ca_cert > "$TLS_PATH"/kafka_ca.crt;
		// curl -XPOST -k -H "X-Vault-Token: $X_VAULT_TOKEN" -d '{"common_name": "'$COMMON_NAME'.kompitech.com"}' "$VAULT_ADDRESS"/v1/kafka-client-int/issue/TLS > /tmp/tlsresponse;
		// cat /tmp/tlsresponse | jq -r -j .data.certificate > "$TLS_PATH"/kafka_cert.crt;
		// cat /tmp/tlsresponse | jq -r -j .data.private_key > "$TLS_PATH"/kafka_cert.key;
		// fi`
		s := `if [ ! -f "$TLS_PATH"/cert.key ]; then
		curl -k -H "X-Vault-Token: $X_VAULT_TOKEN" "$VAULT_ADDRESS"/v1/"$ORG_ID"-kv/"$CORE_PEER_NAME"/tls > /tmp/tlsresponse;
		cat /tmp/tlsresponse | jq -r -j .data.cert > "$TLS_PATH"/cert.crt;
		cat /tmp/tlsresponse | jq -r -j .data.key > "$TLS_PATH"/cert.key;
		cat /tmp/tlsresponse | jq -r -j .data.ca > "$TLS_PATH"/ca.crt;
		fi`
		cmd = cmd + s
	} else {
		s := `if [ ! -f "$TLS_PATH"/cert.key ]; then
		curl -XPOST -k -H "X-Vault-Token: $X_VAULT_TOKEN" -d '{"common_name": "'$COMMON_NAME'"}' "$VAULT_ADDRESS"/v1/"$ORG_ID"/issue/TLS > /tmp/tlsresponse;
		cat /tmp/tlsresponse | jq -r -j .data.certificate > "$TLS_PATH"/cert.crt;
		cat /tmp/tlsresponse | jq -r -j .data.private_key > "$TLS_PATH"/cert.key;
		cat /tmp/tlsresponse | jq -r -j .data.issuing_ca > "$TLS_PATH"/ca.crt;
		fi`
		cmd = cmd + s
	}

	return []corev1.Container{
		{
			Name:  "vault-init",
			Image: "everpeace/curl-jq",
			Command: []string{
				"/bin/sh",
				"-c",
				cmd,
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "certificate",
					MountPath: vault.MSPPath,
					SubPath:   "data/msp",
				},
				{
					Name:      "certificate",
					MountPath: vault.TLSPath,
					SubPath:   "data/tls",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name: "CORE_PEER_ID",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
				{
					Name: "CORE_PEER_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.labels['name']",
						},
					},
				},
				{
					Name:  "REGION_NAME",
					Value: vault.Cluster,
				},
				{
					Name:  "COMMON_NAME",
					Value: vault.CommonName,
				},
				{
					Name:  "ORG_ID",
					Value: vault.Organization,
				},
				{
					Name:  "TLS_PATH",
					Value: vault.TLSPath,
				},
				{
					Name:  "MSP_PATH",
					Value: vault.MSPPath,
				},
				{
					Name:  "VAULT_ADDRESS",
					Value: vault.VaultAddress,
				},
			},
		},
	}
}
