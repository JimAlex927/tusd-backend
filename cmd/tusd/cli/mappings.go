package cli

import (
	"encoding/json"
	"fmt"
	handler_ "github.com/JimAlex927/tusd-backend/pkg/handler"
	"os"
)

// Config 是 JSON 文件的结构体映射
type Config struct {
	PathMappings map[string]string `json:"pathMappings"`
}

func LoadMappingFile() Config {
	// 1. 读取 JSON 配置文件内容
	configPath := Flags.MappingFilePath
	fmt.Println("Loading mappings from ", configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}
	}

	// 2. 解析为 Config 结构体
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return Config{}
	}
	handler_.PathMappings = cfg.PathMappings
	fmt.Println(handler_.PathMappings)
	return cfg
}
