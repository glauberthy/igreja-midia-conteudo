//go:build unix

package retencao

import "syscall"

// EspacoLivre devolve os bytes disponíveis no sistema de arquivos que contém `path`.
// Usa os blocos disponíveis ao usuário comum (Bavail), não os totais livres (Bfree), que
// incluem a reserva do root — o que faria a checagem ser otimista justo quando o disco
// está no limite.
func EspacoLivre(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
