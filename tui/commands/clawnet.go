package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/clawnet"
)

// RunClawNet 执行 clawnet 子命令。
func RunClawNet(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui clawnet <status|peers|tasks|credits>")
	}
	client := clawnet.NewClient()
	if !client.IsRunning() {
		return fmt.Errorf("ClawNet daemon is not running. Start it first or enable clawnet_enabled in config.")
	}
	switch args[0] {
	case "status":
		return clawnetStatus(client, args[1:])
	case "peers":
		return clawnetPeers(client, args[1:])
	case "tasks":
		return clawnetTasks(client, args[1:])
	case "credits":
		return clawnetCredits(client, args[1:])
	default:
		return NewUsageError("unknown clawnet action: %s", args[0])
	}
}

func clawnetStatus(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	status, err := client.GetStatus()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(status)
	}
	fmt.Printf("ClawNet Status:\n")
	fmt.Printf("  PeerID:    %s\n", status.PeerID)
	fmt.Printf("  Peers:     %d\n", status.Peers)
	fmt.Printf("  UnreadDM:  %d\n", status.UnreadDM)
	fmt.Printf("  Version:   %s\n", status.Version)
	fmt.Printf("  Uptime:    %s\n", status.Uptime)
	return nil
}

func clawnetPeers(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet peers", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	peers, err := client.GetPeers()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(peers)
	}
	if len(peers) == 0 {
		fmt.Println("No peers connected.")
		return nil
	}
	fmt.Printf("%-20s %-20s %-10s %s\n", "PEER ID", "ADDR", "LATENCY", "COUNTRY")
	fmt.Println(strings.Repeat("-", 65))
	for _, p := range peers {
		fmt.Printf("%-20s %-20s %-10s %s\n",
			TruncateDisplay(p.PeerID, 20), TruncateDisplay(p.Addr, 20), p.Latency, p.Country)
	}
	return nil
}

func clawnetTasks(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks", flag.ExitOnError)
	status := fs.String("status", "", "按状态过滤 (open/assigned/completed)")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	tasks, err := client.ListTasks(*status)
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(tasks)
	}
	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}
	fmt.Printf("%-20s %-10s %-8s %s\n", "ID", "STATUS", "REWARD", "TITLE")
	fmt.Println(strings.Repeat("-", 70))
	for _, t := range tasks {
		fmt.Printf("%-20s %-10s %-8.1f %s\n",
			TruncateDisplay(t.ID, 20), t.TaskStatus, t.Reward, TruncateDisplay(t.Title, 30))
	}
	return nil
}

func clawnetCredits(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet credits", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	credits, err := client.GetCredits()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(credits)
	}
	fmt.Printf("ClawNet Credits:\n")
	fmt.Printf("  Balance:  %.2f\n", credits.Balance)
	fmt.Printf("  Tier:     %s\n", credits.Tier)
	fmt.Printf("  Energy:   %.2f\n", credits.Energy)
	return nil
}
