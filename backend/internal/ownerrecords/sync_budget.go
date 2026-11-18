package ownerrecords

import "time"

// A stage budget is not a long transaction or a per-SQL timeout. Each stage
// checkpoints bounded units and reserves time to record its progress.
const syncPhaseBudget = 10 * time.Second
const syncSourceBudget = 45 * time.Second
const syncRepairBudget = 60 * time.Second
const syncProjectionBudget = 60 * time.Second
const syncOwnerBudget = 10 * time.Second
const syncReportBudget = 30 * time.Second
const nativeReadBudget = 30 * time.Second
const nativeScanBudget = 90 * time.Second
const nativeItemBudget = 45 * time.Second
const nativeCheckpointBudget = 2 * time.Second
const syncMetadataBudget = 2 * time.Second

// Include child exit, the bounded guard-release write and response delivery.
const nativeReaderWaitBudget = nativeReadBudget + syncMetadataBudget + time.Second
const syncDiagnosticBudget = 5 * time.Second
const syncDiagnosticWriteBudget = time.Second
const syncDiagnosticFinalReserve = time.Second

// Display window, not an extra worker or a requirement to wait this long.
// The nominal serial phase allowances total 405s, plus bounded checkpoints.
const syncBatchDisplayWindow = 8 * time.Minute
