package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// IcanhazlbService represents the CRD object structure
type IcanhazlbService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              IcanhazlbServiceSpec `json:"spec"`
}

// IcanhazlbServiceSpec represents the spec structure within the CRD
type IcanhazlbServiceSpec struct {
	EndpointSlices IcanhazlbEndpointSlice `json:"endpointSlices"`
	Services       IcanhazlbServiceData   `json:"services"`
	Ingresses      IcanhazlbIngressData   `json:"ingresses"`
}

// IcanhazlbEndpointSlice represents the endpointSlices structure within the CRD
type IcanhazlbEndpointSlice struct {
	Name        string                 `json:"name"`
	AddressType string                 `json:"addressType"`
	Ports       []IcanhazlbServicePort `json:"ports"`
	Endpoints   []IcanhazlbEndpoint    `json:"endpoints"`
	Labels      map[string]string      `json:"labels"`
}

// IcanhazlbServicePort represents the port structure within endpointSlices
type IcanhazlbServicePort struct {
	Name string `json:"name"`
	Port int32  `json:"port"`
}

// IcanhazlbEndpoint represents the endpoint structure within endpointSlices
type IcanhazlbEndpoint struct {
	Addresses []string `json:"addresses"`
}

// IcanhazlbServiceData represents the services structure within the CRD
type IcanhazlbServiceData struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	IPFamilies []string               `json:"ipFamilies"`
	Ports      []IcanhazlbServicePort `json:"ports"`
	Labels     map[string]string      `json:"labels"`
}

// IcanhazlbIngressData represents the ingresses structure within the CRD
type IcanhazlbIngressData struct {
	Name             string            `json:"name"`
	Annotations      map[string]string `json:"annotations"`
	IngressClassName string            `json:"ingressClassName"`
	Rules            []IcanhazlbRule   `json:"rules"`
}

// IcanhazlbRule represents the rule structure within ingresses
type IcanhazlbRule struct {
	Host string        `json:"host"`
	HTTP IcanhazlbHTTP `json:"http"`
}

// IcanhazlbHTTP represents the HTTP structure within ingresses
type IcanhazlbHTTP struct {
	Paths []IcanhazlbPath `json:"paths"`
}

// IcanhazlbPath represents the path structure within HTTP
type IcanhazlbPath struct {
	Path     string           `json:"path"`
	PathType string           `json:"pathType"`
	Backend  IcanhazlbBackend `json:"backend"`
}

// IcanhazlbBackend represents the backend structure within path
type IcanhazlbBackend struct {
	Service IcanhazlbServiceBackend `json:"service"`
}

// IcanhazlbServiceBackend represents the service structure within backend
type IcanhazlbServiceBackend struct {
	Name string               `json:"name"`
	Port IcanhazlbBackendPort `json:"port"`
}

// IcanhazlbBackendPort represents the port structure within service
type IcanhazlbBackendPort struct {
	Number int32 `json:"number"`
}

func main() {
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	hostname := extractHostnameFromRequest(r)
	ipAddress := parseIPAddressFromHostname(hostname)

	createCRDInKubernetes(ipAddress, hostname)

	response := map[string]string{
		"ipAddress": ipAddress,
		"hostname":  hostname,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func extractHostnameFromRequest(r *http.Request) string {
	host := strings.SplitN(r.Host, ":", 2)[0]
	return host
}

func parseIPAddressFromHostname(hostname string) string {
	// Regular expression to match IPv4 address in various forms
	regex := regexp.MustCompile(`(\d{1,3}[-._]){3}\d{1,3}`)

	match := regex.FindString(hostname)
	matchFrDash := strings.ReplaceAll(match, "-", ".")
	matchFrUnderscore := strings.ReplaceAll(matchFrDash, "_", ".")

	return matchFrUnderscore
}

func createCRDInKubernetes(ipAddress, hostname string) {
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		log.Fatalf("Failed to build Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	crdName := "icanhazlb-" + ipAddress
	endpointSlicesName := "icanhazlb-" + ipAddress + "-svc"
	servicesName := "icanhazlb-" + ipAddress + "-svc"
	ingressesName := "icanhazlb-" + ipAddress + "-ing"

	icanhazlbService := &IcanhazlbService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "service.icanhazlb.com/v1alpha1",
			Kind:       "IcanhazlbService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      crdName,
			Namespace: "default",
		},
		Spec: IcanhazlbServiceSpec{
			EndpointSlices: IcanhazlbEndpointSlice{
				Name:        endpointSlicesName,
				AddressType: "IPv4",
				Ports: []IcanhazlbServicePort{
					{
						Name: "http",
						Port: 80,
					},
				},
				Endpoints: []IcanhazlbEndpoint{
					{
						Addresses: []string{
							ipAddress,
						},
					},
				},
				Labels: map[string]string{
					"kubernetes.io/service-name": "icanhazlb-" + ipAddress + "-svc",
				},
			},
			Services: IcanhazlbServiceData{
				Name:       servicesName,
				Type:       "ClusterIP",
				IPFamilies: []string{"IPv4"},
				Ports: []IcanhazlbServicePort{
					{
						Name: "http",
						Port: 80,
					},
				},
				Labels: map[string]string{
					"kubernetes.io/service-name": "icanhazlb-" + ipAddress + "-svc",
				},
			},
			Ingresses: IcanhazlbIngressData{
				Name: ingressesName,
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/upstream-vhost": "retro.adrenlinerush.net",
				},
				IngressClassName: "nginx",
				Rules: []IcanhazlbRule{
					{
						Host: hostname,
						HTTP: IcanhazlbHTTP{
							Paths: []IcanhazlbPath{
								{
									Path:     "/",
									PathType: "ImplementationSpecific",
									Backend: IcanhazlbBackend{
										Service: IcanhazlbServiceBackend{
											Name: servicesName,
											Port: IcanhazlbBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = clientset.
		CoreV1().
		RESTClient().
		Post().
		AbsPath("/apis/service.icanhazlb.com/v1alpha1/namespaces/default/icanhazlbservices").
		Body(icanhazlbService).
		DoRaw(context.TODO()) // Pass the context.TODO() as the argument
	if err != nil {
		log.Fatalf("Failed to create CRD: %v", err)
	}

	log.Printf("Created CRD for IP address %s and hostname %s", ipAddress, hostname)
}
