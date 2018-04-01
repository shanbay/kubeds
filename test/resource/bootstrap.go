package resource

import (
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
)

// MakeBootstrap creates a bootstrap envoy configuration
// https://www.envoyproxy.io/docs/envoy/latest/configuration/overview/v2_overview.html#bootstrap-configuration
func MakeBootstrap(controlPort, adminPort uint32) *bootstrap.Bootstrap {
	return &bootstrap.Bootstrap{
		Node: &core.Node{
			Id:      "test-id",
			Cluster: "test-cluster",
		},
		Admin: bootstrap.Admin{
			AccessLogPath: "/dev/null",
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: localhost,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: adminPort,
						},
					},
				},
			},
		},
		StaticResources: &bootstrap.Bootstrap_StaticResources{
			Clusters: []v2.Cluster{{
				Name:           XdsCluster,
				ConnectTimeout: 5 * time.Second,
				Type:           v2.Cluster_STATIC,
				Hosts: []*core.Address{{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: localhost,
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: controlPort,
							},
						},
					},
				}},
				LbPolicy:             v2.Cluster_ROUND_ROBIN,
				Http2ProtocolOptions: &core.Http2ProtocolOptions{},
			}},
		},
	}
}
