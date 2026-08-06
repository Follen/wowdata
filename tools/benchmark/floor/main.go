// Command floor computes a resource-aware critical-path lower bound for a
// measured wowdata command. It consumes observations only and never reads a
// production cache.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

const schema = "wowdata.command-floor.v1"

type input struct {
	Command       string      `json:"command"`
	Protocol      string      `json:"protocol"`
	CaseID        string      `json:"caseId,omitempty"`
	StableCaseKey string      `json:"stableCaseKey,omitempty"`
	RunID         string      `json:"runId,omitempty"`
	MatrixSha256  string      `json:"matrixSha256,omitempty"`
	Calibration   calibration `json:"calibration"`
	Actual        actual      `json:"actual,omitempty"`
	Nodes         []node      `json:"nodes"`
}

type calibration struct {
	ProcessStartNanos     int64                               `json:"processStartNanos"`
	NetworkBytesPerSecond float64                             `json:"networkBytesPerSecond"`
	DiskBytesPerSecond    float64                             `json:"diskBytesPerSecond"`
	CPUWorkBytesPerSecond float64                             `json:"cpuWorkBytesPerSecond"`
	OutputBytesPerSecond  float64                             `json:"outputBytesPerSecond"`
	ResourceClasses       map[string]resourceClassCalibration `json:"resourceClasses,omitempty"`
}

type resourceClassCalibration struct {
	Resource       string  `json:"resource"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
}

type actual struct {
	P50Nanos int64 `json:"p50Nanos,omitempty"`
	P95Nanos int64 `json:"p95Nanos,omitempty"`
	MaxNanos int64 `json:"maxNanos,omitempty"`
}

type node struct {
	ID                         string   `json:"id"`
	DependsOn                  []string `json:"dependsOn,omitempty"`
	FixedNanos                 int64    `json:"fixedNanos,omitempty"`
	CriticalPathRTTNanos       int64    `json:"criticalPathRttNanos,omitempty"`
	RequiredUniqueNetworkBytes int64    `json:"requiredUniqueNetworkBytes,omitempty"`
	RequiredDiskBytes          int64    `json:"requiredDiskBytes,omitempty"`
	RequiredCPUWorkBytes       int64    `json:"requiredCpuWorkBytes,omitempty"`
	UnavoidableOutputBytes     int64    `json:"unavoidableOutputBytes,omitempty"`
	NetworkResourceClass       string   `json:"networkResourceClass,omitempty"`
	SharedNetworkResourceClass string   `json:"sharedNetworkResourceClass,omitempty"`
	DiskResourceClass          string   `json:"diskResourceClass,omitempty"`
	CPUResourceClass           string   `json:"cpuResourceClass,omitempty"`
	NetworkBytesPerSecond      float64  `json:"networkBytesPerSecond,omitempty"`
	DiskBytesPerSecond         float64  `json:"diskBytesPerSecond,omitempty"`
	CPUWorkBytesPerSecond      float64  `json:"cpuWorkBytesPerSecond,omitempty"`
}

type report struct {
	Schema                 string                 `json:"schema"`
	Command                string                 `json:"command"`
	Protocol               string                 `json:"protocol"`
	CaseID                 string                 `json:"caseId,omitempty"`
	StableCaseKey          string                 `json:"stableCaseKey,omitempty"`
	RunID                  string                 `json:"runId,omitempty"`
	MatrixSha256           string                 `json:"matrixSha256,omitempty"`
	FloorNanos             int64                  `json:"floorNanos"`
	FloorMilliseconds      float64                `json:"floorMilliseconds"`
	ProcessStartNanos      int64                  `json:"processStartNanos"`
	CriticalPathNanos      int64                  `json:"criticalPathNanos"`
	CriticalPath           []string               `json:"criticalPath"`
	AggregateResourceFloor resourceFloor          `json:"aggregateResourceFloor"`
	ClassCapacityFloors    []classCapacityFloor   `json:"classCapacityFloors"`
	DominantResourceClass  string                 `json:"dominantResourceClass"`
	Totals                 totals                 `json:"totals"`
	Actual                 *actualReport          `json:"actual,omitempty"`
	Derivation             *matrixFloorDerivation `json:"derivation,omitempty"`
}

type classCapacityFloor struct {
	Class          string  `json:"class"`
	Resource       string  `json:"resource"`
	WorkBytes      int64   `json:"workBytes"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
	Nanos          int64   `json:"nanos"`
}

type resourceFloor struct {
	NetworkNanos int64  `json:"networkNanos"`
	DiskNanos    int64  `json:"diskNanos"`
	CPUNanos     int64  `json:"cpuNanos"`
	Dominant     string `json:"dominant"`
	Nanos        int64  `json:"nanos"`
}

type totals struct {
	RequiredUniqueNetworkBytes int64 `json:"requiredUniqueNetworkBytes"`
	RequiredDiskBytes          int64 `json:"requiredDiskBytes"`
	RequiredCPUWorkBytes       int64 `json:"requiredCpuWorkBytes"`
	UnavoidableOutputBytes     int64 `json:"unavoidableOutputBytes"`
}

type actualReport struct {
	P50Nanos      int64   `json:"p50Nanos,omitempty"`
	P95Nanos      int64   `json:"p95Nanos,omitempty"`
	MaxNanos      int64   `json:"maxNanos,omitempty"`
	P50FloorRatio float64 `json:"p50FloorRatio,omitempty"`
	P95FloorRatio float64 `json:"p95FloorRatio,omitempty"`
	P50Pass       bool    `json:"p50Pass"`
	P95Pass       bool    `json:"p95Pass"`
}

type nodeState struct {
	duration int64
	finish   int64
	parent   string
	visiting bool
	done     bool
}

type classAccumulator struct {
	resource string
	rate     float64
	bytes    int64
}

func main() {
	inputPath := flag.String("input", "", "workload DAG JSON (required)")
	outputPath := flag.String("output", "", "write report JSON to this path (stdout when empty)")
	matrixPath := flag.String("matrix-report", "", "generate floors from a command matrix run report")
	localCalibrationPath := flag.String("local-calibration", "", "local calibration report for matrix generation")
	networkCalibrationPath := flag.String("network-calibration", "", "network calibration report for matrix generation")
	processCalibrationPath := flag.String("process-calibration", "", "process-start calibration report for matrix generation")
	outputDir := flag.String("output-dir", "", "matrix floor output directory")
	flag.Parse()
	if *matrixPath != "" {
		if err := generateMatrixFloors(matrixGenerationConfig{
			MatrixReport: *matrixPath, LocalCalibration: *localCalibrationPath,
			NetworkCalibration: *networkCalibrationPath, ProcessCalibration: *processCalibrationPath,
			OutputDir: *outputDir,
		}); err != nil {
			fatal(err)
		}
		return
	}
	if *inputPath == "" {
		fatal(errors.New("-input is required"))
	}
	data, err := os.ReadFile(*inputPath)
	if err != nil {
		fatal(err)
	}
	var value input
	if err := json.Unmarshal(data, &value); err != nil {
		fatal(err)
	}
	result, err := calculate(value)
	if err != nil {
		fatal(err)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	if *outputPath == "" {
		_, err = os.Stdout.Write(encoded)
	} else {
		err = os.WriteFile(*outputPath, encoded, 0o644)
	}
	if err != nil {
		fatal(err)
	}
}

func calculate(in input) (report, error) {
	if strings.TrimSpace(in.Command) == "" || len(in.Nodes) == 0 {
		return report{}, errors.New("command and at least one DAG node are required")
	}
	if in.Calibration.ProcessStartNanos < 0 {
		return report{}, errors.New("processStartNanos must not be negative")
	}
	byID := make(map[string]node, len(in.Nodes))
	states := make(map[string]*nodeState, len(in.Nodes))
	classes := make(map[string]*classAccumulator)
	if err := validateClassCalibrations(in.Calibration); err != nil {
		return report{}, err
	}
	var sum totals
	for _, current := range in.Nodes {
		if current.ID == "" {
			return report{}, errors.New("DAG node id must not be empty")
		}
		if _, exists := byID[current.ID]; exists {
			return report{}, fmt.Errorf("duplicate DAG node %q", current.ID)
		}
		if current.FixedNanos < 0 || current.CriticalPathRTTNanos < 0 || current.RequiredUniqueNetworkBytes < 0 || current.RequiredDiskBytes < 0 || current.RequiredCPUWorkBytes < 0 || current.UnavoidableOutputBytes < 0 {
			return report{}, fmt.Errorf("DAG node %q contains a negative cost", current.ID)
		}
		byID[current.ID] = current
		states[current.ID] = &nodeState{}
		sum.RequiredUniqueNetworkBytes += current.RequiredUniqueNetworkBytes
		sum.RequiredDiskBytes += current.RequiredDiskBytes
		sum.RequiredCPUWorkBytes += current.RequiredCPUWorkBytes
		sum.UnavoidableOutputBytes += current.UnavoidableOutputBytes
	}
	for id, current := range byID {
		for _, dependency := range current.DependsOn {
			if _, exists := byID[dependency]; !exists {
				return report{}, fmt.Errorf("DAG node %q depends on unknown node %q", id, dependency)
			}
		}
		duration, err := nodeDuration(current, in.Calibration, classes)
		if err != nil {
			return report{}, err
		}
		states[id].duration = duration
	}

	var visit func(string) (int64, error)
	visit = func(id string) (int64, error) {
		state := states[id]
		if state.done {
			return state.finish, nil
		}
		if state.visiting {
			return 0, fmt.Errorf("DAG contains a cycle at %q", id)
		}
		state.visiting = true
		var dependencyFinish int64
		for _, dependency := range byID[id].DependsOn {
			finish, err := visit(dependency)
			if err != nil {
				return 0, err
			}
			if finish > dependencyFinish || finish == dependencyFinish && (state.parent == "" || dependency < state.parent) {
				dependencyFinish, state.parent = finish, dependency
			}
		}
		state.finish = dependencyFinish + state.duration
		state.visiting, state.done = false, true
		return state.finish, nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var terminal string
	var critical int64
	for _, id := range ids {
		finish, err := visit(id)
		if err != nil {
			return report{}, err
		}
		if finish > critical || finish == critical && (terminal == "" || id < terminal) {
			critical, terminal = finish, id
		}
	}
	path := make([]string, 0, len(in.Nodes))
	for terminal != "" {
		path = append(path, terminal)
		terminal = states[terminal].parent
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	classFloors, dominantClass, classCapacity := capacityFloors(classes)
	resources := aggregateFloor(classFloors)
	floor := in.Calibration.ProcessStartNanos + max64(critical, classCapacity)
	result := report{
		Schema: schema, Command: in.Command, Protocol: in.Protocol, CaseID: in.CaseID, StableCaseKey: in.StableCaseKey, RunID: in.RunID, MatrixSha256: in.MatrixSha256, FloorNanos: floor,
		FloorMilliseconds: float64(floor) / 1e6, ProcessStartNanos: in.Calibration.ProcessStartNanos,
		CriticalPathNanos: critical, CriticalPath: path, AggregateResourceFloor: resources,
		ClassCapacityFloors: classFloors, DominantResourceClass: dominantClass, Totals: sum,
	}
	if floor > 0 && (in.Actual.P50Nanos > 0 || in.Actual.P95Nanos > 0 || in.Actual.MaxNanos > 0) {
		result.Actual = &actualReport{
			P50Nanos: in.Actual.P50Nanos, P95Nanos: in.Actual.P95Nanos, MaxNanos: in.Actual.MaxNanos,
			P50FloorRatio: float64(in.Actual.P50Nanos) / float64(floor),
			P95FloorRatio: float64(in.Actual.P95Nanos) / float64(floor),
			P50Pass:       in.Actual.P50Nanos > 0 && float64(in.Actual.P50Nanos) <= 1.25*float64(floor),
			P95Pass:       in.Actual.P95Nanos > 0 && float64(in.Actual.P95Nanos) <= 1.50*float64(floor),
		}
	}
	return result, nil
}

func nodeDuration(value node, rates calibration, classes map[string]*classAccumulator) (int64, error) {
	networkRate, err := registerClass(classes, rates, "network", value.NetworkResourceClass, value.NetworkBytesPerSecond, value.RequiredUniqueNetworkBytes)
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	network, err := durationFor(value.RequiredUniqueNetworkBytes, networkRate, "network")
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	if value.RequiredUniqueNetworkBytes > 0 && strings.TrimSpace(value.SharedNetworkResourceClass) != "" {
		if _, err := registerClass(classes, rates, "network", value.SharedNetworkResourceClass, 0, value.RequiredUniqueNetworkBytes); err != nil {
			return 0, fmt.Errorf("DAG node %q shared network capacity: %w", value.ID, err)
		}
	}
	diskRate, err := registerClass(classes, rates, "disk", value.DiskResourceClass, value.DiskBytesPerSecond, value.RequiredDiskBytes)
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	disk, err := durationFor(value.RequiredDiskBytes, diskRate, "disk")
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	cpuRate, err := registerClass(classes, rates, "cpu", value.CPUResourceClass, value.CPUWorkBytesPerSecond, value.RequiredCPUWorkBytes)
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	cpu, err := durationFor(value.RequiredCPUWorkBytes, cpuRate, "CPU")
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	output, err := durationFor(value.UnavoidableOutputBytes, rates.OutputBytesPerSecond, "output")
	if err != nil {
		return 0, fmt.Errorf("DAG node %q: %w", value.ID, err)
	}
	return value.FixedNanos + value.CriticalPathRTTNanos + max64(network, disk, cpu) + output, nil
}

func validateClassCalibrations(rates calibration) error {
	for name, class := range rates.ResourceClasses {
		if strings.TrimSpace(name) == "" {
			return errors.New("resource class name must not be empty")
		}
		resource := strings.ToLower(strings.TrimSpace(class.Resource))
		if resource != "network" && resource != "disk" && resource != "cpu" {
			return fmt.Errorf("resource class %q has invalid resource %q", name, class.Resource)
		}
		if class.BytesPerSecond <= 0 || math.IsNaN(class.BytesPerSecond) || math.IsInf(class.BytesPerSecond, 0) {
			return fmt.Errorf("resource class %q throughput must be positive", name)
		}
	}
	return nil
}

func registerClass(classes map[string]*classAccumulator, rates calibration, resource, name string, override float64, bytes int64) (float64, error) {
	if bytes == 0 {
		if override < 0 || math.IsNaN(override) || math.IsInf(override, 0) {
			return 0, fmt.Errorf("%s throughput override must be positive", resource)
		}
		return 0, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = resource
	}
	rate := defaultRate(rates, resource)
	if configured, ok := rates.ResourceClasses[name]; ok {
		if strings.ToLower(strings.TrimSpace(configured.Resource)) != resource {
			return 0, fmt.Errorf("resource class %q is %s, not %s", name, configured.Resource, resource)
		}
		rate = configured.BytesPerSecond
	}
	if override != 0 {
		if override < 0 || math.IsNaN(override) || math.IsInf(override, 0) {
			return 0, fmt.Errorf("%s throughput override must be positive", resource)
		}
		if _, configured := rates.ResourceClasses[name]; configured && override != rate {
			return 0, fmt.Errorf("resource class %q throughput override conflicts with calibration", name)
		}
		rate = override
	}
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, fmt.Errorf("%s throughput must be positive for non-zero work in class %q", resource, name)
	}
	if existing, ok := classes[name]; ok {
		if existing.resource != resource {
			return 0, fmt.Errorf("resource class %q is used for both %s and %s", name, existing.resource, resource)
		}
		if existing.rate != rate {
			return 0, fmt.Errorf("resource class %q has inconsistent throughput", name)
		}
		existing.bytes += bytes
	} else {
		classes[name] = &classAccumulator{resource: resource, rate: rate, bytes: bytes}
	}
	return rate, nil
}

func defaultRate(rates calibration, resource string) float64 {
	switch resource {
	case "network":
		return rates.NetworkBytesPerSecond
	case "disk":
		return rates.DiskBytesPerSecond
	default:
		return rates.CPUWorkBytesPerSecond
	}
}

func capacityFloors(classes map[string]*classAccumulator) ([]classCapacityFloor, string, int64) {
	names := make([]string, 0, len(classes))
	for name := range classes {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]classCapacityFloor, 0, len(names))
	var dominant string
	var maximum int64
	for _, name := range names {
		class := classes[name]
		nanos, _ := durationFor(class.bytes, class.rate, class.resource)
		result = append(result, classCapacityFloor{Class: name, Resource: class.resource, WorkBytes: class.bytes, BytesPerSecond: class.rate, Nanos: nanos})
		if nanos > maximum || nanos == maximum && (dominant == "" || name < dominant) {
			dominant, maximum = name, nanos
		}
	}
	return result, dominant, maximum
}

func aggregateFloor(classes []classCapacityFloor) resourceFloor {
	result := resourceFloor{Dominant: "network"}
	for _, class := range classes {
		switch class.Resource {
		case "network":
			result.NetworkNanos = max64(result.NetworkNanos, class.Nanos)
		case "disk":
			result.DiskNanos = max64(result.DiskNanos, class.Nanos)
		case "cpu":
			result.CPUNanos = max64(result.CPUNanos, class.Nanos)
		}
	}
	result.Nanos = result.NetworkNanos
	if result.DiskNanos > result.Nanos {
		result.Dominant, result.Nanos = "disk", result.DiskNanos
	}
	if result.CPUNanos > result.Nanos {
		result.Dominant, result.Nanos = "cpu", result.CPUNanos
	}
	return result
}

func durationFor(bytes int64, bytesPerSecond float64, name string) (int64, error) {
	if bytes == 0 {
		return 0, nil
	}
	if bytesPerSecond <= 0 || math.IsNaN(bytesPerSecond) || math.IsInf(bytesPerSecond, 0) {
		return 0, fmt.Errorf("%s throughput must be positive for non-zero work", name)
	}
	return int64(math.Ceil(float64(bytes) / bytesPerSecond * 1e9)), nil
}

func max64(values ...int64) int64 {
	var result int64
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "floor:", err)
	os.Exit(1)
}
