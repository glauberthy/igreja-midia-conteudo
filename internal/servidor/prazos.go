package servidor

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Prazos limita quanto cada etapa pode durar. Não são limites de desempenho: são a
// garantia de que TODO pedido chega a um estado terminal.
//
// Por que isso importa mais do que parece: a limpeza (spec-06) trata pedido não-terminal
// como intocável. Um pedido travado em "baixando-video" protegeria os ~900 MB dele do
// disco para sempre, além de ocupar o operador com um spinner infinito — exatamente o que
// a spec-05 quis evitar. Sem prazo, "em curso" e "morto" são indistinguíveis.
//
// Os valores são folgados de propósito: no pior pedido medido, o download levou ~86s e o
// render ~3s por Short. Os prazos abaixo são mais de dez vezes isso, então uma rede lenta
// (mas viva) não é interrompida — só a rede TRAVADA é.
type Prazos struct {
	Legenda   time.Duration
	Selecao   time.Duration
	Video     time.Duration
	Renderize time.Duration
}

// PrazosPadrao é o que o servidor usa quando Opcoes.Prazos vem zerado.
func PrazosPadrao() Prazos {
	return Prazos{
		Legenda:   10 * time.Minute, // legenda são poucos MB; 10min já é anomalia
		Selecao:   30 * time.Minute, // o harness faz várias chamadas ao modelo
		Video:     20 * time.Minute, // 900 MB a 750 KB/s ainda cabem
		Renderize: 15 * time.Minute, // ~3s por Short medido
	}
}

// comPadroes preenche campos zerados com o padrão, para o chamador poder ajustar só um.
func (p Prazos) comPadroes() Prazos {
	d := PrazosPadrao()
	if p.Legenda <= 0 {
		p.Legenda = d.Legenda
	}
	if p.Selecao <= 0 {
		p.Selecao = d.Selecao
	}
	if p.Video <= 0 {
		p.Video = d.Video
	}
	if p.Renderize <= 0 {
		p.Renderize = d.Renderize
	}
	return p
}

// ErrPrazoEstourado marca a etapa interrompida por tempo. O chamador o reconhece com
// errors.Is para NÃO prefixar a mensagem (ela já se explica sozinha).
var ErrPrazoEstourado = errors.New("prazo da etapa estourado")

type erroPrazo struct{ msg string }

func (e *erroPrazo) Error() string      { return e.msg }
func (e *erroPrazo) Is(alvo error) bool { return alvo == ErrPrazoEstourado }

// etapaComPrazo roda uma etapa com prazo. Quando o prazo estoura, a mensagem NOMEIA o que
// travou e por quanto tempo — o operador precisa saber que foi interrupção por tempo, e
// não um defeito do vídeo dele.
func etapaComPrazo(pai context.Context, rotulo string, d time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(pai, d)
	defer cancel()
	err := fn(ctx)
	// DeadlineExceeded pode vir embrulhado (exec, http), daí o errors.Is. Confere o ctx
	// também: um processo morto por sinal devolve "signal: killed", sem o sentinela.
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded) {
		return &erroPrazo{fmt.Sprintf(
			"%s passou de %s e foi interrompida — a rede ou o serviço travou; tente de novo",
			rotulo, formatarPrazo(d))}
	}
	return err
}

// comPrefixo dá contexto ao erro, EXCETO quando é estouro de prazo: nesse caso a mensagem
// já nomeia a etapa, e prefixar produziria "falha na seleção: a seleção passou de 30min".
func comPrefixo(prefixo string, err error) string {
	if errors.Is(err, ErrPrazoEstourado) {
		return err.Error()
	}
	return prefixo + err.Error()
}

func formatarPrazo(d time.Duration) string {
	if m := int(d.Minutes()); m > 0 {
		return fmt.Sprintf("%dmin", m)
	}
	return d.String()
}
