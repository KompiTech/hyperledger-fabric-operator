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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io/ioutil"
	"strings"
	"time"

	"github.com/pkg/errors"

	"go.etcd.io/etcd/clientv3"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// etcdEndpoint = "localhost:2379" // for local run
	etcdEndpoint = "etcd-client.etcd:2379"
	etcdTimeout  = 15
)

var log = logf.Log.WithName("resources_dns")

func reverse(s string) string {
	n := len(s)
	runes := make([]rune, n)
	for _, rune := range s {
		n--
		runes[n] = rune
	}
	return string(runes[n:])
}

func getSkyDNSEntry(fqdn string) string {

	// split into slice by dot .
	addressSlice := strings.Split(fqdn, ".")
	reverseSlice := []string{}

	for i := range addressSlice {
		octet := addressSlice[len(addressSlice)-1-i]
		reverseSlice = append(reverseSlice, octet)
	}

	S := strings.Join(reverseSlice, "/")
	// log.Info("DNS name: %v", S)
	//S = reverse(strings.Replace(reverse(S), ".", "/", strings.Count(S, "-")-1))
	//log.Printf("DNS name reversed: %v", S)
	return S
}

// CheckDNS checks if DNS entry exists and creates it if not
func CheckDNS(clusterIP, fqdn string) error {
	if fqdn == "" {
		return errors.Errorf("Fqdn cannot be empty for clusterIP: %v", clusterIP)
	}
	if clusterIP == "" {
		return errors.Errorf("Fqdn cannot be empty for fqdn: %v", fqdn)
	}
	// log.Info("checking dns entry for fqdn %v, current IP is: %v", fqdn, clusterIP)
	// tlsConfig, err := getTLSConfig()
	// if err != nil {
	// 	log.Printf("ERROR getting tls config for etcd client during checking dns for service: %v", svc)
	// 	return err
	// }
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdEndpoint},
		DialTimeout: etcdTimeout * time.Second,
		// TLS:         tlsConfig, // uncomment once tls on etcd is desired
	})
	if err != nil {
		log.Error(err, "getting etcd client during fqdn:"+fqdn+" check failed")
		return err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	fqdn = "/skydns/" + getSkyDNSEntry(fqdn)

	// get current IP entry in etcd
	resp, err := cli.Get(ctx, fqdn)
	defer cancel()

	// log.Info("DNS response %v, error: %v", resp, err)
	if err != nil || resp == nil {
		log.Error(err, "getting fqdn: "+fqdn+" from skydns failed")
		return errors.Errorf("ERROR: while getting fqdn: %v from skydns", fqdn)
	}
	// check if update is needed
	if resp.Count > 0 {
		result := make(map[string]string)
		err = json.Unmarshal(resp.Kvs[0].Value, &result)
		if result["host"] != clusterIP {
			// log.Info("Updating entry for fqdn %v, current IP is: %v", fqdn, clusterIP)
			err = updateDNSRecord(fqdn, clusterIP)
			if err != nil {
				log.Error(err, "Update entry failed for fqdn "+fqdn+", current IP is: "+clusterIP+", etcdIP: "+result["host"])
				return err
			}
		}
	} else {
		// log.Info("Creating entry for fqdn %v, current IP is: %v", fqdn, clusterIP)
		err = updateDNSRecord(fqdn, clusterIP)
		if err != nil {
			log.Error(err, "Create entry failed for fqdn "+fqdn+", current IP is: "+clusterIP)
			return err
		}
	}
	return nil
}

func getTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair("../cert.crt", "../cert.key")
	if err != nil {
		return nil, err
	}
	cacert, err := ioutil.ReadFile("../ca.crt")
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM([]byte(cacert))

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: false,
	}
	return tlsConfig, nil
}

func updateDNSRecord(key, ip string) error {
	// tlsConfig, err := getTLSConfig()
	// if err != nil {
	// 	log.Printf("ERROR getting tls config for etcd client during updating key: %v", key)
	// 	return err
	// }
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdEndpoint},
		DialTimeout: etcdTimeout * time.Second,
		// TLS:         tlsConfig,
	})
	if err != nil {
		log.Error(err, "getting etcd client during key: "+key+" put failed")
		return err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	_, err = cli.Put(ctx, key, "{\"host\":\""+ip+"\"}")
	defer cancel()
	// log.Info("Put response: %v, error: %v, key: %v", resp, err, key)

	if err != nil {
		log.Error(err, "updating record in etcd key: "+key+" failed")
		return err
	}

	// log.Info("DNS record: %v updated to %v", key, ip)
	return nil
}

// DeleteDNS deletes DNS record
func DeleteDNS(fqdn string) error {
	if fqdn == "" {
		return errors.Errorf("ERROR invalid fqdn while deleting dns record: %v", fqdn)
	}
	// log.Info("deleting dns entry for key: %v", fqdn)
	// tlsConfig, err := getTLSConfig()
	// if err != nil {
	// 	log.Printf("ERROR getting tls config for etcd client during deleting key: %v", fqdn)
	// 	return err
	// }
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdEndpoint},
		DialTimeout: etcdTimeout * time.Second,
		// TLS:         tlsConfig,
	})
	if err != nil {
		return errors.Errorf("ERROR getting etcd client during key: %v deletion", fqdn)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	fqdn = "/skydns/" + getSkyDNSEntry(fqdn)

	// Delete key from etcd
	resp, err := cli.Delete(ctx, fqdn)
	cancel()
	if err != nil || resp == nil {
		log.Error(err, "deleting key "+fqdn+" failed")
		return err
	}
	return nil
}
