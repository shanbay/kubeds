package leizu

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

// LoadDefaultSettingsFor load default config
func LoadDefaultSettingsFor(v *viper.Viper) {
	home, err := homedir.Dir()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defaultKubeConfig := filepath.Join(home, ".kube", "config")

	v.SetDefault("outCluster", false)
	v.SetDefault("kubeConfigPath", defaultKubeConfig)
	v.SetDefault("namespace", "")
	v.SetDefault("xdsPort", 6666)
	v.SetDefault("ads", false)
}
