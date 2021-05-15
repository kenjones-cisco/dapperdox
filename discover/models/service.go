package models

import (
	"strings"
)

// Service describes an Istio service (e.g., catalog.mystore.com:8080)
// Each service has a fully qualified domain name (FQDN) and one or more
// ports where the service is listening for connections. *Optionally*, a
// service can have a single load balancer/virtual IP address associated
// with it, such that the DNS queries for the FQDN resolves to the virtual
// IP address (a load balancer IP).
//
// E.g., in kubernetes, a service foo is associated with
// foo.default.svc.cluster.local hostname, has a virtual IP of 10.0.1.1 and
// listens on ports 80, 8080.
type Service struct {
	// Hostname of the service, e.g. "catalog.mystore.com"
	Hostname string `json:"hostname"`

	// Address specifies the service IPv4 address of the load balancer
	Address string `json:"address,omitempty"`

	// Ports is the set of network ports where the service is listening for
	// connections
	Ports PortList `json:"ports"`

	// ExternalName is only set for external services and holds the external
	// service DNS name.  External services are name-based solution to represent
	// external service instances as a service inside the cluster.
	ExternalName string `json:"external,omitempty"`

	// LoadBalancingDisabled indicates that no load balancing should be done for this service.
	LoadBalancingDisabled bool `json:"-"`
}

// External predicate checks whether the service is external.
func (s *Service) External() bool {
	return s.ExternalName != ""
}

// Port represents a network port where a service is listening for
// connections. The port should be annotated with the type of protocol
// used by the port.
type Port struct {
	// Name ascribes a human readable name for the port object. When a
	// service has multiple ports, the name field is mandatory
	Name string `json:"name,omitempty"`

	// Port number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service. See networkEndpoint definition below.
	Port int `json:"port"`

	// Protocol to be used for the port.
	Protocol Protocol `json:"protocol"`
}

// PortList is a set of ports.
type PortList []*Port

// get retrieves a port declaration by name.
func (ports PortList) get(name string) (*Port, bool) {
	for _, port := range ports {
		if port.Name == name {
			return port, true
		}
	}

	return nil, false
}

// Protocol defines network protocols for ports.
type Protocol string

const (
	// ProtocolGRPC declares that the port carries gRPC traffic.
	ProtocolGRPC Protocol = "GRPC"
	// ProtocolHTTPS declares that the port carries HTTPS traffic.
	ProtocolHTTPS Protocol = "HTTPS"
	// ProtocolHTTP2 declares that the port carries HTTP/2 traffic.
	ProtocolHTTP2 Protocol = "HTTP2"
	// ProtocolHTTP declares that the port carries HTTP/1.1 traffic.
	// Note that HTTP/1.0 or earlier may not be supported by the proxy.
	ProtocolHTTP Protocol = "HTTP"
	// ProtocolTCP declares the the port uses TCP.
	// This is the default protocol for a service port.
	ProtocolTCP Protocol = "TCP"
	// ProtocolUnsupported - value to signify that the protocol is unsupported.
	ProtocolUnsupported Protocol = "UnsupportedProtocol"
)

// ConvertCaseInsensitiveStringToProtocol converts a case-insensitive protocol to Protocol.
func ConvertCaseInsensitiveStringToProtocol(protocolAsString string) Protocol {
	switch strings.ToLower(protocolAsString) {
	case "tcp":
		return ProtocolTCP
	case "grpc":
		return ProtocolGRPC
	case "http":
		return ProtocolHTTP
	case "http2":
		return ProtocolHTTP2
	case "https":
		return ProtocolHTTPS
	}

	return ProtocolUnsupported
}

// IsHTTP is true for protocols that use HTTP as transport protocol.
func (p Protocol) IsHTTP() bool {
	switch p {
	case ProtocolHTTP, ProtocolHTTP2, ProtocolGRPC:
		return true
	default:
		return false
	}
}
