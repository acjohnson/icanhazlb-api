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
	"regexp"
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
		svcFriendlyHostname := strings.ReplaceAll(hostname, "_", "-")
		svcFriendlyIp := strings.ReplaceAll(ipAddress, ".", "-")

		err := createCRDInKubernetes(clientset, ipAddress, svcFriendlyHostname, svcFriendlyIp)
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
	// Regular expression pattern for matching IP address formats
	ipv4RE := `((\d{1,3}\.){3}\d{1,3}|(\d{1,3}-){3}\d{1,3}|(\d{1,3}_){3}\d{1,3}|(\d{1,3}[-_.]){3}\d{1,3})`

	// Match the IP address using the regular expression
	re := regexp.MustCompile(ipv4RE)
	match := re.FindString(hostname)

	if match != "" {
		// Remove any non-numeric characters from the matched IP address
		ip := strings.Map(func(r rune) rune {
			if r == '-' || r == '_' || r == '.' {
				return '.'
			}
			return r
		}, match)

		// Validate and return the parsed IPv4 address
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil || !parsedIP.To4().Equal(parsedIP) {
			fmt.Printf("Failed to parse IPv4 address from hostname: %s\n", hostname)
			return ""
		}
		return parsedIP.String()
	}

	fmt.Printf("Failed to parse IP address from hostname: %s\n", hostname)
	return ""
}

func createCRDInKubernetes(clientset *kubernetes.Clientset, ipAddress, hostname string, svcFriendlyIp string) error {
	icanhazlbService := &IcanhazlbService{
		TypeMeta: v1.TypeMeta{
			APIVersion: fmt.Sprintf("%s/%s", icanhazlbAPIGroup, icanhazlbAPIVersion),
			Kind:       "IcanhazlbService",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("icanhazlb-%s", svcFriendlyIp),
			Namespace: "default",
		},
		Spec: IcanhazlbServiceSpec{
			EndpointSlices: IcanhazlbEndpointSlices{
				Name:        fmt.Sprintf("icanhazlb-%s-svc", svcFriendlyIp),
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
					"kubernetes.io/service-name": fmt.Sprintf("icanhazlb-%s-svc", svcFriendlyIp),
				},
			},
			Services: IcanhazlbServices{
				Name:       fmt.Sprintf("icanhazlb-%s-svc", svcFriendlyIp),
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
					"kubernetes.io/service-name": fmt.Sprintf("icanhazlb-%s-svc", svcFriendlyIp),
				},
			},
			Ingresses: IcanhazlbIngresses{
				Name: fmt.Sprintf("icanhazlb-%s-ing", svcFriendlyIp),
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
											Name: fmt.Sprintf("icanhazlb-%s-svc", svcFriendlyIp),
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

	request := clientset.CoreV1().RESTClient().Post().
		AbsPath(fmt.Sprintf("/apis/%s/%s/namespaces/default/%s", icanhazlbAPIGroup, icanhazlbAPIVersion, icanhazlbServicePlural)).
		Body(raw)

	response := request.Do(context.TODO())
	if response.Error() != nil {
		return fmt.Errorf("failed to create CRD: %v", response.Error())
	}

	rawResponse, err := response.Raw()
	if err != nil {
		return fmt.Errorf("failed to read raw response: %v", err)
	}

	var decodedJSON struct {
		Metadata struct {
			ManagedFields []struct {
				Operation *string `json:"operation"`
			} `json:"managedFields"`
		} `json:"metadata"`
	}

	if err := json.Unmarshal(rawResponse, &decodedJSON); err != nil {
		return fmt.Errorf("failed to unmarshal JSON response: %v", err)
	}

	if len(decodedJSON.Metadata.ManagedFields) > 0 && decodedJSON.Metadata.ManagedFields[0].Operation != nil {
		// The operation field is present, indicating success
		fmt.Println("Success")
	} else {
		// The operation field is not present, indicating failure
		fmt.Println("Failure")
	}

	return nil
}
