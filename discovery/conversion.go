package discovery

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/kenjones-cisco/dapperdox/discovery/model"
)

func convertPort(port v1.ServicePort) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Port),
		Protocol: convertProtocol(port.Name, port.Protocol),
	}
}

func convertService(svc v1.Service, domainSuffix string) *model.Service {
	addr, external := "", ""
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		addr = svc.Spec.ClusterIP
	}

	if svc.Spec.Type == v1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
		external = svc.Spec.ExternalName
	}

	ports := make([]*model.Port, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		ports = append(ports, convertPort(port))
	}

	loadBalancingDisabled := addr == "" && external == "" // headless services should not be load balanced

	return &model.Service{
		Hostname:              serviceHostname(svc.Name, svc.Namespace, domainSuffix),
		Ports:                 ports,
		Address:               addr,
		ExternalName:          external,
		LoadBalancingDisabled: loadBalancingDisabled,
	}
}

// serviceHostname produces FQDN for a k8s service
func serviceHostname(name, namespace, domainSuffix string) string {
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, domainSuffix)
}

// keyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func keyFunc(name, namespace string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

/*
// parseHostname extracts service name and namespace from the service hostname
func parseHostname(hostname string) (name, namespace string, err error) {
	parts := strings.Split(hostname, ".")
	if len(parts) < 2 {
		err = fmt.Errorf("missing service name and namespace from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	namespace = parts[1]
	return
}
*/

// convertProtocol from k8s protocol and port name
func convertProtocol(name string, proto v1.Protocol) model.Protocol {
	out := model.ProtocolTCP
	switch proto {
	case v1.ProtocolUDP:
		out = model.ProtocolUDP
	case v1.ProtocolTCP:
		prefix := name
		i := strings.Index(name, "-")
		if i >= 0 {
			prefix = name[:i]
		}
		protocol := model.ConvertToProtocol(prefix)
		if protocol != model.ProtocolUDP && protocol != model.ProtocolUnsupported {
			out = protocol
		}
	}
	return out
}

func convertProbePort(c v1.Container, handler *v1.Handler) (*model.Port, error) {
	if handler == nil {
		return nil, nil
	}

	var protocol model.Protocol
	var portVal intstr.IntOrString
	var port int

	// Only one type of handler is allowed by Kubernetes (HTTPGet or TCPSocket)
	if handler.HTTPGet != nil {
		portVal = handler.HTTPGet.Port
		protocol = model.ProtocolHTTP
	} else if handler.TCPSocket != nil {
		portVal = handler.TCPSocket.Port
		protocol = model.ProtocolTCP
	} else {
		return nil, nil
	}

	switch portVal.Type {
	case intstr.Int:
		port = portVal.IntValue()
		return &model.Port{
			Name:     "mgmt-" + strconv.Itoa(port),
			Port:     port,
			Protocol: protocol,
		}, nil
	case intstr.String:
		for _, named := range c.Ports {
			if named.Name == portVal.String() {
				port = int(named.ContainerPort)
				return &model.Port{
					Name:     "mgmt-" + strconv.Itoa(port),
					Port:     port,
					Protocol: protocol,
				}, nil
			}
		}
		return nil, fmt.Errorf("missing named port %q", portVal)
	default:
		return nil, fmt.Errorf("incorrect port type %q", portVal)
	}
}

// convertProbesToPorts returns a PortList consisting of the ports where the
// pod is configured to do Liveness and Readiness probes
func convertProbesToPorts(t *v1.PodSpec) (model.PortList, error) {
	set := make(map[string]*model.Port)
	var errs error
	for _, container := range t.Containers {
		for _, probe := range []*v1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			if probe == nil {
				continue
			}

			p, err := convertProbePort(container, &probe.Handler)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else if p != nil && set[p.Name] == nil {
				// Deduplicate along the way. We don't differentiate between HTTP vs TCP mgmt ports
				set[p.Name] = p
			}
		}
	}

	mgmtPorts := make(model.PortList, 0, len(set))
	for _, p := range set {
		mgmtPorts = append(mgmtPorts, p)
	}
	sort.Slice(mgmtPorts, func(i, j int) bool { return mgmtPorts[i].Port < mgmtPorts[j].Port })

	return mgmtPorts, errs
}
