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
	"log"
	"strings"
	"time"

	"github.com/pkg/errors"

	"go.etcd.io/etcd/clientv3"
)

const (
	// etcdEndpoint = "localhost:2379" // for local run
	etcdEndpoint = "etcd-client.etcd:2379"
	etcdTimeout  = 15
)

func Reverse(s string) string {
	n := len(s)
	runes := make([]rune, n)
	for _, rune := range s {
		n--
		runes[n] = rune
	}
	return string(runes[n:])
}

func GetSkyDnsEntry(fqdn string) string {

	// split into slice by dot .
	addressSlice := strings.Split(fqdn, ".")
	reverseSlice := []string{}

	for i := range addressSlice {
		octet := addressSlice[len(addressSlice)-1-i]
		reverseSlice = append(reverseSlice, octet)
	}

	S := strings.Join(reverseSlice, "/")
	log.Printf("DNS name: %v", S)
	//S = Reverse(strings.Replace(Reverse(S), ".", "/", strings.Count(S, "-")-1))
	//log.Printf("DNS name reversed: %v", S)
	return S
}

func CheckDNS(clusterIP, fqdn string) error {
	if fqdn == "" {
		return errors.Errorf("Fqdn cannot be empty for clusterIP: %v", clusterIP)
	}
	if clusterIP == "" {
		return errors.Errorf("Fqdn cannot be empty for fqdn: %v", fqdn)
	}
	log.Printf("checking dns entry for fqdn %v, current IP is: %v", fqdn, clusterIP)
	// tlsConfig, err := GetTlsConfig()
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
		log.Printf("ERROR getting etcd client during fqdn: %v check", fqdn)
		return err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	fqdn = "/skydns/" + GetSkyDnsEntry(fqdn)

	// get current IP entry in etcd
	resp, err := cli.Get(ctx, fqdn)
	defer cancel()

	log.Printf("DNS response %v, error: %v", resp, err)
	if err != nil || resp == nil {
		log.Printf("ERROR: while getting fqdn: %v from skydns", fqdn)
		return errors.Errorf("ERROR: while getting fqdn: %v from skydns", fqdn)
	}
	// check if update is needed
	if resp.Count > 0 {
		result := make(map[string]string)
		err = json.Unmarshal(resp.Kvs[0].Value, &result)
		if result["host"] != clusterIP {
			log.Printf("Updating entry for fqdn %v, current IP is: %v", fqdn, clusterIP)
			err = UpdateDnsRecord(fqdn, clusterIP)
			if err != nil {
				log.Printf("Update entry failed for fqdn %v, current IP is: %v, etcdIP: %v, err: %v", fqdn, clusterIP, result["host"], err)
				return err
			}
		}
	} else {
		log.Printf("Creating entry for fqdn %v, current IP is: %v", fqdn, clusterIP)
		err = UpdateDnsRecord(fqdn, clusterIP)
		if err != nil {
			log.Printf("Create entry failed for fqdn %v, current IP is: %v, err: %v", fqdn, clusterIP, err)
			return err
		}
	}
	return nil
}

func GetTlsConfig() (*tls.Config, error) {
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

func UpdateDnsRecord(key, ip string) error {
	// tlsConfig, err := GetTlsConfig()
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
		log.Printf("ERROR getting etcd client during key: %v put", key)
		return err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	resp, err := cli.Put(ctx, key, "{\"host\":\""+ip+"\"}")
	defer cancel()
	log.Printf("Put response: %v, error: %v, key: %v", resp, err, key)

	if err != nil {
		log.Printf("ERROR updating record in etcd key: %v", key)
		return err
	}

	log.Printf("DNS record: %v updated to %v", key, ip)
	return nil
}

func DeleteDNS(fqdn string) error {
	if fqdn == "" {
		return errors.Errorf("ERROR invalid fqdn while deleting dns record: %v", fqdn)
	}
	log.Printf("deleting dns entry for key: %v", fqdn)
	// tlsConfig, err := GetTlsConfig()
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
	fqdn = "/skydns/" + GetSkyDnsEntry(fqdn)

	// Delete key from etcd
	resp, err := cli.Delete(ctx, fqdn)
	cancel()
	if err != nil || resp == nil {
		return log.Output(1, "ERROR: while deleting key: "+fqdn)
	}
	return nil
}
