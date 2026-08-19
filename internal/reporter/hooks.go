package reporter

import "github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"

// QueueHookEvent is used by deterministic Debian Installer hooks. Before the
// late HARDENING hook is queued, it synchronously captures native installer
// evidence so OS_INSTALLING and PROFILE_APPLYING cannot be overtaken by the
// late-command stage merely because the background monitor has not ticked yet.
func QueueHookEvent(stage lifecycle.Stage, source lifecycle.Source, message, errorCode string) error {
	if stage == lifecycle.StageHardening && source == lifecycle.SourceInstaller {
		if err := detectNativeInstallerEvidence(); err != nil {
			return err
		}
	}
	return QueueEvent(stage, source, message, errorCode)
}
