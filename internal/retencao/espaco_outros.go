//go:build !unix

package retencao

import "fmt"

// EspacoLivre não é suportado fora de sistemas unix. O chamador trata o erro como
// "não sei o espaço" e segue sem a verificação preventiva (melhor que impedir o uso).
func EspacoLivre(path string) (int64, error) {
	return 0, fmt.Errorf("consulta de espaço livre não suportada neste sistema")
}
