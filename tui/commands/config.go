package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

// RunConfig 执行 config 子命令。
func RunConfig(args []string, hubURL, token string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: maclaw-tui config <get|set>")
	}

	client := NewHubClient(hubURL, token)
	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()

	switch args[0] {
	case "get":
		return configGet(client, args[1:])
	case "set":
		return configSet(client, args[1:])
	default:
		return fmt.Errorf("unknown config action: %s", args[0])
	}
}

func configGet(client *HubClient, args []string) error {
	fs := flag.NewFlagSet("config get", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	key := fs.Arg(0)
	payload := map[string]string{}
	if key != "" {
		payload["key"] = key
	}

	data, err := client.Request("cli.config_get", payload)
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		var v interface{}
		json.Unmarshal(data, &v)
		return enc.Encode(v)
	}

	// 简单文本输出
	var kv map[string]interface{}
	if err := json.Unmarshal(data, &kv); err != nil {
		// 单值
		fmt.Println(string(data))
		return nil
	}
	for k, v := range kv {
		fmt.Printf("%s = %v\n", k, v)
	}
	return nil
}

func configSet(client *HubClient, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: config set <key> <value>")
	}
	key, value := args[0], args[1]

	_, err := client.Request("cli.config_set", map[string]string{
		"key":   key,
		"value": value,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", key, value)
	return nil
}
