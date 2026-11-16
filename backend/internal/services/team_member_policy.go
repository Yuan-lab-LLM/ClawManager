package services

// Public APIs must reject new standalone member removals. Internal cleanup of
// already-started deletions remains available through the existing engine.
const TeamMemberRemovalUnsupportedCode = "TEAM_MEMBER_REMOVAL_UNSUPPORTED"
const TeamMemberRemovalUnsupportedMessage = "不支持单独删除团队成员；如需删除，请删除整个 Team"
