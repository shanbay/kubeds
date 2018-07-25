package leizu

import (
	"context"
	"net"
	"sync"
	envoyApiV2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyApiV2Core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	k8sApiV1Core "k8s.io/api/core/v1"
	k8sApiMetaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// TODO: currently we only support XDS for one envoy nodeID
const nodeID = "defaultEnvoyNode"

// Hasher is a single cache key hash.
type Hasher struct {
}

// ID function that always returns the same value.
func (h Hasher) ID(node *envoyApiV2Core.Node) string {
	return nodeID
}

var (
	once sync.Once
	app  *Application
)

// SimpleKubeClient create a *kubernetes.Clientset by using viper config
func SimpleKubeClient(config *viper.Viper) (*kubernetes.Clientset, error) {
	if config == nil {
		config = viper.GetViper()
	}
	var (
		kubeConfig *rest.Config
		err        error
	)
	if viper.GetBool("outCluster") {
		kubeConfigPath := viper.GetString("kubeConfigPath")
		logrus.WithField("kubeConfigPath", kubeConfigPath).Infoln("using out cluster config")
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		if err != nil {
			logrus.WithError(err).Fatalln("load config failed")
		}
	} else {
		kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			logrus.WithError(err).Fatalln("load config failed")
		}
	}
	logrus.WithFields(logrus.Fields{
		"host":      kubeConfig.Host,
		"username":  kubeConfig.Username,
		"userAgent": kubeConfig.UserAgent,
	}).Infoln("k8s config was loaded")
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		logrus.WithError(err).Fatalln("make k8s client failed")
		return nil, err
	}
	return kubeClient, nil
}

// InitApplication init an application, the program only execute once
func InitApplication(config *viper.Viper) *Application {
	once.Do(func() {
		if config == nil {
			config = viper.GetViper()
		}
		app = &Application{
			logger: logrus.New(),
			ctx:    context.Background(),
			Config: config,
		}
		app.logger.Formatter = &logrus.TextFormatter{
			ForceColors:   true,
			FullTimestamp: true,
		}
		// init snapshotCache
		app.cache = cache.NewSnapshotCache(config.GetBool("ads"), Hasher{}, app.logger)
		snapShot := cache.NewSnapshot(
			"0",
			[]cache.Resource{},
			[]cache.Resource{},
			[]cache.Resource{},
			[]cache.Resource{},
		)

		if err := app.cache.SetSnapshot(nodeID, snapShot); err != nil {
			app.logger.WithError(err).Errorln("Init snapshot failed ")
		}
		app.server = xds.NewServer(app.cache, nil)
		app.grpcServer = grpc.NewServer()
		app.snapshot = make(map[string]envoyApiV2.ClusterLoadAssignment)

		envoyApiV2.RegisterEndpointDiscoveryServiceServer(app.grpcServer, app.server)

		kubeClient, err := SimpleKubeClient(config)
		if err != nil {
			app.logger.WithError(err).Fatalln("get kube client failed")
		}
		app.KubeClient = kubeClient
	})
	return app
}

// Application is program entry
type Application struct {
	logger     *logrus.Logger
	Config     *viper.Viper
	ctx        context.Context
	cache      cache.SnapshotCache
	server     xds.Server
	grpcServer *grpc.Server
	KubeClient *kubernetes.Clientset
	snapshot   map[string]envoyApiV2.ClusterLoadAssignment
}

// RunXds run xds server
func (a *Application) RunXds() {
	xdsPort := a.Config.GetString("xdsPort")
	lis, err := net.Listen("tcp", ":"+xdsPort)
	if err != nil {
		a.logger.WithError(err).Fatalln("failed to listen")
	}
	a.logger.WithField("xdsPort", xdsPort).Infoln("start listening grpc")
	if err = a.grpcServer.Serve(lis); err != nil {
		a.logger.WithError(err).Fatalln("serve grpc server failed")
	}
}

// WatchEndpoints watch kubernetes endpoint changes
func (a *Application) WatchEndpoints() {
	// watch k8s cluster endpoints, and set set snapshot after changes
	// 初次监听会返回当前的状态
	// Endpoints 属于一个资源, 每次更新会带上当前所有的 endpoint, 例如当某个部署副本由 1 调整到 3 则会收到两次 MODIFIED 事件
	nameSpace := a.Config.GetString("namespace")
	for {
		endWatcher, err := a.KubeClient.CoreV1().Endpoints(nameSpace).Watch(k8sApiMetaV1.ListOptions{})
		if err != nil {
			a.logger.WithError(err).Fatalln("watch endpoints changes failed")
		}
		a.logger.Infoln("start watching Endpoints events")
		for event := range endWatcher.ResultChan() {
			a.logger.WithField("event", event.Type).Infoln("endpoints event received")
			var healthStatus envoyApiV2Core.HealthStatus
			switch event.Type {
			case watch.Added, watch.Modified:
				healthStatus = envoyApiV2Core.HealthStatus_HEALTHY
			case watch.Deleted, watch.Error:
				healthStatus = envoyApiV2Core.HealthStatus_UNHEALTHY
			default:
				healthStatus = envoyApiV2Core.HealthStatus_UNKNOWN
			}
			endpoints := event.Object.(*k8sApiV1Core.Endpoints)
			clusterName := getClusterNameByEndpoints(endpoints)
			envoyEndpoints := a.Endpoints2ClusterLoadAssignment(endpoints, healthStatus)
			a.snapshot[clusterName] = *envoyEndpoints
			var resources []cache.Resource
			for k := range a.snapshot {
				tmp := a.snapshot[k]
				resources = append(resources, &tmp)
			}
			snapShot := cache.NewSnapshot(
				endpoints.ResourceVersion,
				resources,
				[]cache.Resource{},
				[]cache.Resource{},
				[]cache.Resource{},
			)
			// TODO: dispatch Node
			if err := a.cache.SetSnapshot(nodeID, snapShot); err != nil {
				a.logger.WithError(err).Errorln("SetSnapshot failed ")
			}
			a.logger.WithField("version", endpoints.ResourceVersion).Infoln("set new snapshot")
		}
		a.logger.Infoln("watcher exited!")
	}
}

// Serve start and block the main process
func (a *Application) Serve() {
	go a.RunXds()
	go a.WatchEndpoints()
	<-a.ctx.Done()
	a.grpcServer.GracefulStop()
}

// Endpoints2ClusterLoadAssignment convert Endpoints to ClusterLoadAssignment
func (a *Application) Endpoints2ClusterLoadAssignment(endpoints *k8sApiV1Core.Endpoints, healthStatus envoyApiV2Core.HealthStatus) *envoyApiV2.ClusterLoadAssignment {
	// clusterName format is "svcName.Namespace"
	clusterName := getClusterNameByEndpoints(endpoints)

	lbEndpoints := make([]endpoint.LbEndpoint, 0)
	for _, subset := range endpoints.Subsets {
		for _, port := range subset.Ports {
			for _, address := range subset.Addresses {
				var protocol envoyApiV2Core.SocketAddress_Protocol
				switch port.Protocol {
				case k8sApiV1Core.ProtocolTCP:
					protocol = envoyApiV2Core.TCP
				case k8sApiV1Core.ProtocolUDP:
					protocol = envoyApiV2Core.UDP
				}
				lbEndpoints = append(lbEndpoints, endpoint.LbEndpoint{
					HealthStatus: healthStatus,
					Endpoint: &endpoint.Endpoint{
						Address: &envoyApiV2Core.Address{
							Address: &envoyApiV2Core.Address_SocketAddress{
								SocketAddress: &envoyApiV2Core.SocketAddress{
									Protocol: protocol,
									Address:  address.IP,
									PortSpecifier: &envoyApiV2Core.SocketAddress_PortValue{
										PortValue: uint32(port.Port),
									},
								},
							},
						},
					},
				})
			}
		}
	}

	a.logger.WithFields(logrus.Fields{
		"clusterName":      clusterName,
		"healthStatus":     healthStatus,
		"lbEndPointsCount": len(lbEndpoints),
	}).Infoln("converted k8s endpoints to envoy cluster load assignment")
	return &envoyApiV2.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints: []endpoint.LocalityLbEndpoints{{
			LbEndpoints: lbEndpoints,
		}},
	}
}

func getClusterNameByEndpoints(endpoints *k8sApiV1Core.Endpoints) string {
	return endpoints.ObjectMeta.Name + "." + endpoints.ObjectMeta.Namespace
}
