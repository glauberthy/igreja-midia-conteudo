// Pacote processo roda comandos externos (yt-dlp, ffmpeg) de modo que o cancelamento do
// contexto MATE de fato tudo o que foi criado — não só o processo direto.
//
// Por que isto não é detalhe: quando o prazo de uma etapa estoura, o pedido vai para erro,
// vira terminal e a limpeza apaga o resíduo. Se um neto (o ffmpeg que o yt-dlp criou)
// sobreviver com o arquivo aberto, no Linux o unlink NÃO devolve o espaço — só quando o
// descritor fecha. O log diria "liberei 900 MB" e o `df` continuaria cheio, fazendo o
// próximo pedido falhar por espaço que deveria existir.
//
// Há ainda uma armadilha do os/exec: com Stdout/Stderr apontando para um bytes.Buffer, o
// pacote cria pipes e goroutines de cópia, e Wait() só retorna quando TODOS os escritores
// do pipe fecham — inclusive os netos. Um neto vivo trava o Wait() para sempre, e aí nem
// o prazo resolve. WaitDelay é a rede contra isso.
package processo

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// EsperaAposMatar é quanto Wait() ainda aguarda depois de matar o grupo antes de largar os
// pipes à força. Curto de propósito: a essa altura o processo já levou SIGKILL.
const EsperaAposMatar = 5 * time.Second

// Rodar executa o comando e devolve stdout, stderr e o erro. Ao cancelar o contexto, mata
// o GRUPO de processos e só retorna depois de Wait() — ou seja, quando o comando está
// comprovadamente morto. Quem chama pode limpar o resíduo em seguida com segurança.
func Rodar(ctx context.Context, nome string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, nome, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	// Isola o comando num grupo próprio e faz o cancelamento matar o grupo inteiro.
	// Em sistemas sem grupo de processos vira no-op (comportamento antigo).
	isolarGrupo(cmd)
	cmd.WaitDelay = EsperaAposMatar

	err := cmd.Run() // Run = Start + Wait: ao retornar, o filho já foi colhido
	return out.Bytes(), errb.Bytes(), err
}
