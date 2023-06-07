package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	icanhazlbAPIGroup      = "service.icanhazlb.com"
	icanhazlbAPIVersion    = "v1alpha1"
	icanhazlbServicePlural = "icanhazlbservices"
)

type IcanhazlbService struct {
	v1.TypeMeta   `json:",inline"`
	v1.ObjectMeta `json:"metadata,omitempty"`
	Spec          IcanhazlbServiceSpec `json:"spec"`
}

type IcanhazlbServiceSpec struct {
	EndpointSlices IcanhazlbEndpointSlices `json:"endpointSlices"`
	Services       IcanhazlbServices       `json:"services"`
	Ingresses      IcanhazlbIngresses      `json:"ingresses"`
}

type IcanhazlbEndpointSlices struct {
	Name        string              `json:"name"`
	AddressType string              `json:"addressType"`
	Ports       []IcanhazlbPort     `json:"ports"`
	Endpoints   []IcanhazlbEndpoint `json:"endpoints"`
	Labels      map[string]string   `json:"labels"`
}

type IcanhazlbPort struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type IcanhazlbEndpoint struct {
	Addresses []string `json:"addresses"`
}

type IcanhazlbServices struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	IPFamilies []string          `json:"ipFamilies"`
	Ports      []IcanhazlbPort   `json:"ports"`
	Labels     map[string]string `json:"labels"`
}

type IcanhazlbIngresses struct {
	Name             string                 `json:"name"`
	Annotations      map[string]string      `json:"annotations"`
	IngressClassName string                 `json:"ingressClassName"`
	Rules            []IcanhazlbIngressRule `json:"rules"`
}

type IcanhazlbIngressRule struct {
	Host string        `json:"host"`
	HTTP IcanhazlbHTTP `json:"http"`
}

type IcanhazlbHTTP struct {
	Paths []IcanhazlbHTTPPath `json:"paths"`
}

type IcanhazlbHTTPPath struct {
	Path     string               `json:"path"`
	PathType string               `json:"pathType"`
	Backend  IcanhazlbHTTPBackend `json:"backend"`
}

type IcanhazlbHTTPBackend struct {
	Service IcanhazlbHTTPServiceBackend `json:"service"`
}

type IcanhazlbHTTPServiceBackend struct {
	Name string               `json:"name"`
	Port IcanhazlbBackendPort `json:"port"`
}

type IcanhazlbBackendPort struct {
	Number intstr.IntOrString `json:"number"`
}

var kubeconfig string

func main() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig file")
	flag.Parse()

	// Build the Kubernetes configuration
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("Failed to build Kubernetes configuration: %v", err)
	}

	// Create the Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes clientset: %v", err)
	}

	// Start the HTTP server
	server := &http.Server{
		Addr:    ":8080",
		Handler: createHandler(clientset),
	}

	go func() {
		log.Println("Starting server on port 8080")
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")

	// Gracefully shut down the server
	err = server.Shutdown(context.Background())
	if err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	log.Println("Server stopped.")
}

func createHandler(clientset *kubernetes.Clientset) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hostname := extractHostnameFromRequest(r)
		ipAddress := parseIPAddressFromHostname(hostname)

		err := createCRDInKubernetes(clientset, ipAddress, hostname)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create CRD: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]string{
			"ipAddress": ipAddress,
			"hostname":  hostname,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	return mux
}

func extractHostnameFromRequest(r *http.Request) string {
	hostname := strings.SplitN(r.Host, ":", 2)[0]
	return hostname
}

func parseIPAddressFromHostname(hostname string) string {
	// Extract the IP address from the hostname using a regular expression or other parsing method
	// Here's an example using the net package's ParseIP function to extract the first IPv4 address
	ip := net.ParseIP(hostname)
	if ip == nil {
		log.Printf("Failed to parse IP address from hostname: %s", hostname)
		return ""
	}
	ip = ip.To4()
	if ip == nil {
		log.Printf("Failed to parse IPv4 address from hostname: %s", hostname)
		return ""
	}
	return ip.String()
}

func createCRDInKubernetes(clientset *kubernetes.Clientset, ipAddress, hostname string) error {
	icanhazlbService := &IcanhazlbService{
		TypeMeta: v1.TypeMeta{
			APIVersion: fmt.Sprintf("%s/%s", icanhazlbAPIGroup, icanhazlbAPIVersion),
			Kind:       "IcanhazlbService",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("icanhazlb-%s", ipAddress),
			Namespace: "default",
		},
		Spec: IcanhazlbServiceSpec{
			EndpointSlices: IcanhazlbEndpointSlices{
				Name:        fmt.Sprintf("icanhazlb-%s-svc", ipAddress),
				AddressType: "IPv4",
				Ports: []IcanhazlbPort{
					{
						Name: "http",
						Port: 80,
					},
					// Add more ports if needed
				},
				Endpoints: []IcanhazlbEndpoint{
					{
						Addresses: []string{
							ipAddress,
						},
					},
				},
				Labels: map[string]string{
					"kubernetes.io/service-name": fmt.Sprintf("icanhazlb-%s-svc", ipAddress),
				},
			},
			Services: IcanhazlbServices{
				Name:       fmt.Sprintf("icanhazlb-%s-svc", ipAddress),
				Type:       "ClusterIP",
				IPFamilies: []string{"IPv4"},
				Ports: []IcanhazlbPort{
					{
						Name: "http",
						Port: 80,
					},
					// Add more ports if needed
				},
				Labels: map[string]string{
					"kubernetes.io/service-name": fmt.Sprintf("icanhazlb-%s-svc", ipAddress),
				},
			},
			Ingresses: IcanhazlbIngresses{
				Name: fmt.Sprintf("icanhazlb-%s-ing", ipAddress),
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/upstream-vhost": "retro.adrenlinerush.net",
				},
				IngressClassName: "nginx",
				Rules: []IcanhazlbIngressRule{
					{
						Host: hostname,
						HTTP: IcanhazlbHTTP{
							Paths: []IcanhazlbHTTPPath{
								{
									Path:     "/",
									PathType: "ImplementationSpecific",
									Backend: IcanhazlbHTTPBackend{
										Service: IcanhazlbHTTPServiceBackend{
											Name: fmt.Sprintf("icanhazlb-%s-svc", ipAddress),
											Port: IcanhazlbBackendPort{
												Number: intstr.FromInt(80),
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

	raw, err := json.Marshal(icanhazlbService)
	if err != nil {
		return fmt.Errorf("failed to marshal CRD: %v", err)
	}

	_, err = clientset.CoreV1().RESTClient().Post().
		AbsPath(fmt.Sprintf("/apis/%s/%s/namespaces/default/%s", icanhazlbAPIGroup, icanhazlbAPIVersion, icanhazlbServicePlural)).
		Body(raw).
		Do(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to create CRD: %v", err)
	}

	return nil
}
