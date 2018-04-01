package cmd

import (
	"bytes"
	"io/ioutil"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/shanbay/kubeds"
	"github.com/shanbay/kubeds/test/resource"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	k8sApiMetaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	bootstrapFile string
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "test kubeds",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		app := leizu.InitApplication(viper.GetViper())
		// write bootstrap file
		ns := viper.GetString("namespace")

		bootstrap := resource.MakeBootstrap(uint32(app.Config.GetInt("xdsPort")), 19000)
		services, err := app.KubeClient.CoreV1().Services(ns).List(k8sApiMetaV1.ListOptions{})
		if err != nil {
			logrus.Warnln(err)
		}
		for _, svc := range services.Items {
			clusterName := svc.Name + "." + svc.Namespace
			cluster := resource.MakeCluster(app.Config.GetBool("ads"), clusterName)
			bootstrap.StaticResources.Clusters = append(bootstrap.StaticResources.Clusters, *cluster)
		}

		buf := &bytes.Buffer{}
		if err := (&jsonpb.Marshaler{OrigName: true}).Marshal(buf, bootstrap); err != nil {
			logrus.WithError(err).Fatalln("marshal bootstrap file failed")
		}
		if err := ioutil.WriteFile(bootstrapFile, buf.Bytes(), 0644); err != nil {
			logrus.WithError(err).Fatalln("write bootstrap file failed")
		}
		logrus.WithField("path", bootstrapFile).Infoln("please start envoy with bootstrap file")

		app.Serve()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.Flags().StringVar(&bootstrapFile, "bootstrap", "bootstrap.json", "Bootstrap file name")
}
