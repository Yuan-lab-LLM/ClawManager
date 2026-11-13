// Package workspacedashboard reads the authenticated user's CM workspace facts.
package workspacedashboard

import "time"

type Coverage struct {
	Status string     `json:"status"`
	Note   string     `json:"note,omitempty"`
	Since  *time.Time `json:"since,omitempty"`
}
type Sources struct {
	Recent       Coverage `json:"recent"`
	Instances    Coverage `json:"instances"`
	Tasks        Coverage `json:"tasks"`
	Usage        Coverage `json:"usage"`
	Costs        Coverage `json:"costs"`
	Capacity     Coverage `json:"capacity"`
	Observations Coverage `json:"observations"`
	Activity     Coverage `json:"activity"`
}
type Window struct {
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	PreviousStart time.Time `json:"previous_start"`
	PreviousEnd   time.Time `json:"previous_end"`
}
type Agent struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	InstanceMode     string    `json:"instance_mode"`
	Status           string    `json:"status"`
	State            string    `json:"state"`
	TeamID           *int      `json:"team_id,omitempty"`
	TeamName         string    `json:"team_name,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
	Progress         *float64  `json:"progress"`
	CurrentTaskID    string    `json:"current_task_id,omitempty"`
	CurrentTaskTitle string    `json:"current_task_title,omitempty"`
	CPUCores         float64   `json:"cpu_cores"`
	MemoryGB         float64   `json:"memory_gb"`
	StorageGB        float64   `json:"storage_gb"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type Task struct {
	Key           string     `json:"key"`
	Source        string     `json:"source"`
	ID            string     `json:"id"`
	TeamID        *int       `json:"team_id,omitempty"`
	InstanceID    *int       `json:"instance_id,omitempty"`
	SessionID     string     `json:"session_id,omitempty"`
	Title         string     `json:"title"`
	AgentName     string     `json:"agent_name"`
	Status        string     `json:"status"`
	Progress      *float64   `json:"progress"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	ResultExcerpt string     `json:"result_excerpt,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}
type RecentAgent struct {
	Agent
	LastUsedAt   time.Time `json:"last_used_at"`
	ActivityKind string    `json:"activity_kind"`
}
type CurrencyUsage struct {
	Currency       string   `json:"currency"`
	Amount         *float64 `json:"amount"`
	PreviousAmount *float64 `json:"previous_amount"`
	Estimated      bool     `json:"estimated"`
	Complete       bool     `json:"complete"`
	RecordedCalls  int64    `json:"recorded_calls"`
}
type Usage struct {
	TotalTokens      *int64          `json:"total_tokens"`
	PromptTokens     *int64          `json:"prompt_tokens"`
	CompletionTokens *int64          `json:"completion_tokens"`
	InvocationCount  *int64          `json:"invocation_count"`
	PreviousTokens   *int64          `json:"previous_tokens"`
	Currencies       []CurrencyUsage `json:"currencies"`
}
type UsageCoverage struct {
	Usage Coverage `json:"usage"`
	Costs Coverage `json:"costs"`
}
type UsageDashboard struct {
	Range        string        `json:"range"`
	Timezone     string        `json:"timezone"`
	AsOf         time.Time     `json:"as_of"`
	Window       Window        `json:"window"`
	Usage        Usage         `json:"usage"`
	UsageHistory []UsagePoint  `json:"usage_history"`
	AgentUsage   []AgentUsage  `json:"agent_usage"`
	Coverage     UsageCoverage `json:"coverage"`
}
type UsagePoint struct {
	At     time.Time           `json:"at"`
	Tokens *int64              `json:"tokens"`
	Calls  *int64              `json:"calls"`
	Costs  map[string]*float64 `json:"costs"`
}
type AgentUsage struct {
	InstanceID int                `json:"instance_id"`
	Tokens     int64              `json:"tokens"`
	Costs      map[string]float64 `json:"costs"`
}
type RuntimeCapacity struct {
	Type      string `json:"type"`
	Available *int   `json:"available"`
}
type Capacity struct {
	EligibleSlots      *int              `json:"eligible_slots"`
	Runtimes           []RuntimeCapacity `json:"runtimes"`
	AllocatedCPU       float64           `json:"allocated_cpu"`
	AllocatedMemoryGB  float64           `json:"allocated_memory_gb"`
	AllocatedStorageGB float64           `json:"allocated_storage_gb"`
	MaxInstances       *int              `json:"max_instances"`
	MaxCPU             *float64          `json:"max_cpu"`
	MaxMemoryGB        *float64          `json:"max_memory_gb"`
	MaxStorageGB       *float64          `json:"max_storage_gb"`
}
type Observation struct {
	Offline           *int      `json:"offline"`
	Starting          *int      `json:"starting"`
	At                time.Time `json:"at"`
	Total             *int      `json:"total"`
	Busy              *int      `json:"busy"`
	Idle              *int      `json:"idle"`
	Running           *int      `json:"running"`
	Sleeping          *int      `json:"sleeping"`
	Unknown           *int      `json:"unknown"`
	Failed            *int      `json:"failed"`
	AllocatedCPU      *float64  `json:"allocated_cpu"`
	AllocatedMemoryGB *float64  `json:"allocated_memory_gb"`
	MaxCPU            *float64  `json:"max_cpu"`
	MaxMemoryGB       *float64  `json:"max_memory_gb"`
	EligibleSlots     *int      `json:"eligible_slots"`
}
type Activity struct {
	Date     string `json:"date"`
	Events   int64  `json:"events"`
	Calls    int64  `json:"calls"`
	Tasks    int64  `json:"tasks"`
	Tokens   int64  `json:"tokens"`
	Observed bool   `json:"observed"`
}
type Dashboard struct {
	RecentAgents       []RecentAgent  `json:"recent_agents"`
	RecentTruncated    bool           `json:"recent_truncated"`
	Range              string         `json:"range"`
	Timezone           string         `json:"timezone"`
	AsOf               time.Time      `json:"as_of"`
	Window             Window         `json:"window"`
	Instances          []Agent        `json:"instances"`
	InstancesTotal     int            `json:"instances_total"`
	InstancesTruncated bool           `json:"instances_truncated"`
	Tasks              []Task         `json:"tasks"`
	TasksTruncated     bool           `json:"tasks_truncated"`
	StateCounts        map[string]int `json:"state_counts"`
	Usage              Usage          `json:"usage"`
	UsageHistory       []UsagePoint   `json:"usage_history"`
	AgentUsage         []AgentUsage   `json:"agent_usage"`
	Capacity           Capacity       `json:"capacity"`
	Observations       []Observation  `json:"observations"`
	Activity           []Activity     `json:"activity"`
	Coverage           Sources        `json:"coverage"`
}
