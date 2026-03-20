package im

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

// DeviceServiceFinder adapts device.Service to the DeviceFinder interface
// required by MessageRouter.
type DeviceServiceFinder struct {
	Svc *device.Service
}

// FindOnlineMachineForUser returns the first online machine belonging to the
// given user. If multiple machines are online, the first one found is returned.
func (f *DeviceServiceFinder) FindOnlineMachineForUser(ctx context.Context, userID string) (machineID string, llmConfigured bool, found bool) {
	machines, err := f.Svc.ListMachines(ctx, userID)
	if err != nil {
		return "", false, false
	}
	for _, m := range machines {
		if m.Online {
			return m.MachineID, m.LLMConfigured, true
		}
	}
	return "", false, false
}

// FindAllOnlineMachinesForUser returns all online machines for the user.
func (f *DeviceServiceFinder) FindAllOnlineMachinesForUser(ctx context.Context, userID string) []OnlineMachineInfo {
	machines, err := f.Svc.ListMachines(ctx, userID)
	if err != nil {
		return nil
	}
	var out []OnlineMachineInfo
	for _, m := range machines {
		if m.Online {
			out = append(out, OnlineMachineInfo{
				MachineID:     m.MachineID,
				Name:          m.Name,
				LLMConfigured: m.LLMConfigured,
			})
		}
	}
	return out
}

// FindOnlineMachineByName returns the machine ID matching the given name
// (case-insensitive) for the user.
func (f *DeviceServiceFinder) FindOnlineMachineByName(ctx context.Context, userID, name string) (machineID string, found bool) {
	return f.Svc.FindOnlineMachineByName(userID, name)
}

// SendToMachine sends a JSON-serialisable message to the machine via WebSocket.
func (f *DeviceServiceFinder) SendToMachine(machineID string, msg any) error {
	return f.Svc.SendToMachine(machineID, msg)
}
