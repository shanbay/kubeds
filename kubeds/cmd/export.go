package cmd

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/shanbay/kubeds"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func exportJSON(v interface{}, f string, m os.FileMode) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(f, data, m)
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "export kubernetes resources",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		kubeClient, err := leizu.SimpleKubeClient(viper.GetViper())
		if err != nil {
			logrus.WithError(err).Fatalln("get kube client failed")
		}
		ns := viper.GetString("namespace")

		services, err := kubeClient.CoreV1().Services(ns).List(metaV1.ListOptions{})
		if err != nil {
			logrus.Warnln(err)
		}
		err = exportJSON(services, "services.json", os.ModePerm)
		if err != nil {
			logrus.Warnln(err)
		}
		logrus.Infoln("services have been exported")

		pods, err := kubeClient.CoreV1().Pods(ns).List(metaV1.ListOptions{})
		if err != nil {
			logrus.Warnln(err)
		}
		err = exportJSON(pods, "pods.json", os.ModePerm)
		if err != nil {
			logrus.Warnln(err)
		}
		logrus.Infoln("pods have been exported")

		endpoints, err := kubeClient.CoreV1().Endpoints(ns).List(metaV1.ListOptions{})
		if err != nil {
			logrus.Warnln(err)
		}
		err = exportJSON(endpoints, "endpoints.json", os.ModePerm)
		if err != nil {
			logrus.Warnln(err)
		}
		logrus.Infoln("endpoints have been exported")
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
}
