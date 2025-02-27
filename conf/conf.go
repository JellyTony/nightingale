package conf

import (
	"fmt"
	"os"
	"strings"

	"github.com/ccfos/nightingale/v6/alert/aconf"
	"github.com/ccfos/nightingale/v6/center/cconf"
	"github.com/ccfos/nightingale/v6/pkg/cfg"
	"github.com/ccfos/nightingale/v6/pkg/httpx"
	"github.com/ccfos/nightingale/v6/pkg/logx"
	"github.com/ccfos/nightingale/v6/pkg/ormx"
	"github.com/ccfos/nightingale/v6/pushgw/pconf"
	"github.com/ccfos/nightingale/v6/storage"
)

type ConfigType struct {
	Global GlobalConfig
	Log    logx.Config
	HTTP   httpx.Config
	DB     ormx.DBConfig
	Redis  storage.RedisConfig

	Pushgw pconf.Pushgw
	Alert  aconf.Alert
	Center cconf.Center
}

type GlobalConfig struct {
	RunMode string
}

func InitConfig(configDir, cryptoKey string) (*ConfigType, error) {
	var config = new(ConfigType)

	if err := cfg.LoadConfigByDir(configDir, config); err != nil {
		return nil, fmt.Errorf("failed to load configs of directory: %s error: %s", configDir, err)
	}

	config.Pushgw.PreCheck()
	config.Alert.PreCheck()
	config.Center.PreCheck()

	err := decryptConfig(config, cryptoKey)
	if err != nil {
		return nil, err
	}

	if config.Pushgw.DatasourceId == 0 {
		return nil, fmt.Errorf("datasourceId is 0")
	}

	if config.Alert.Heartbeat.IP == "" {
		// auto detect
		// config.Alert.Heartbeat.IP = fmt.Sprint(GetOutboundIP())
		// 自动获取IP在有些环境下容易出错，这里用hostname+pid来作唯一标识

		hostname, err := os.Hostname()
		if err != nil {
			fmt.Println("failed to get hostname:", err)
			os.Exit(1)
		}

		if strings.Contains(hostname, "localhost") {
			fmt.Println("Warning! hostname contains substring localhost, setting a more unique hostname is recommended")
		}

		config.Alert.Heartbeat.IP = hostname

		// if config.Alert.Heartbeat.IP == "" {
		// 	fmt.Println("heartbeat ip auto got is blank")
		// 	os.Exit(1)
		// }
	}

	config.Alert.Heartbeat.Endpoint = fmt.Sprintf("%s:%d", config.Alert.Heartbeat.IP, config.HTTP.Port)

	return config, nil
}
