//go:build !unix

package processo

import "os/exec"

// isolarGrupo é no-op fora de unix: sem grupo de processos, o cancelamento padrão do
// os/exec (matar só o processo direto) é o que há. O WaitDelay continua valendo.
func isolarGrupo(cmd *exec.Cmd) {}
