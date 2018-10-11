package client

import (
	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func LoadConfig(configFileFrom, configFileName, configContext string) (*rest.Config, error) {
	var config *rest.Config
	var err error

	switch configFileFrom {
	case "in-cluster":
		config, err = rest.InClusterConfig()
	case "file":
		var configAPI *clientcmdapi.Config
		configAPI, err = clientcmd.LoadFromFile(configFileName)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load REST client configuration from file %q", configFileName)
		}
		config, err = clientcmd.NewDefaultClientConfig(*configAPI, &clientcmd.ConfigOverrides{
			CurrentContext: configContext,
		}).ClientConfig()
	default:
		err = errors.New("invalid value for 'client config from' parameter")
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load REST client configuration from %q", configFileFrom)
	}
	return config, nil
}
