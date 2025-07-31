package cmd

import "test-task1/models"

const (
	configPath = "config.yaml"
)

func main() {
	cfg := models.MustLoad(configPath)

}
