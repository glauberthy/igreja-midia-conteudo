package servidor

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	Renderize time.Duration
	// VideoSemProgresso é o prazo do DOWNLOAD DO VÍDEO, medido de outra forma: não é o
	// tempo total, é quanto ele pode ficar SEM ESCREVER UM BYTE antes de ser abortado.
	//
	// Um teto fixo não serve aqui, porque o pior caso do download é o produto de duas
	// variáveis que medimos variar muito: tamanho (maior visto: 994 MB; um culto de 2h
	// daria ~1,8 GB) e throughput (3,3 a 23,4 MB/s — 7x). 1,8 GB a 3,3 MB/s são ~9
	// minutos: um teto de 15-20 min daria margem de ~2x, não os 10x das outras etapas, e
	// mataria um download legítimo de um culto longo em rede ruim.
	//
	// Medir progresso é imune ao tamanho do arquivo: um download vivo, mesmo a 10 KB/s,
	// escreve algo a cada minuto. Só o TRAVADO fica sem escrever nada.
	VideoSemProgresso time.Duration
	// VideoTeto é uma rede de segurança absoluta contra patologia do próprio watchdog
	// (ex.: yt-dlp em loop escrevendo lixo). Folgado: não deve ser alcançado nunca.
	VideoTeto time.Duration
}

// PrazosPadrao é o que o servidor usa quando Opcoes.Prazos vem zerado.
func PrazosPadrao() Prazos {
	return Prazos{
		Legenda:           10 * time.Minute, // legenda são poucos MB; 10min já é anomalia
		Selecao:           30 * time.Minute, // o harness faz várias chamadas ao modelo
		Renderize:         15 * time.Minute, // ~3s por Short medido
		VideoSemProgresso: 5 * time.Minute,  // a 3,3 MB/s (o pior medido) seriam ~1 GB
		VideoTeto:         2 * time.Hour,    // rede de segurança; ~11x o pior caso realista
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
	if p.VideoSemProgresso <= 0 {
		p.VideoSemProgresso = d.VideoSemProgresso
	}
	if p.VideoTeto <= 0 {
		p.VideoTeto = d.VideoTeto
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

// etapaComProgresso roda uma etapa vigiando o CRESCIMENTO de uma pasta em vez de cravar um
// tempo total. Aborta quando `semProgresso` passa sem um único byte novo, ou quando o teto
// absoluto estoura. Ver o comentário de Prazos.VideoSemProgresso para o porquê.
//
// A medição é o tamanho somado da pasta do pedido: funciona com os fragmentos paralelos do
// yt-dlp (vários .part crescendo ao mesmo tempo), que um watchdog de arquivo único perderia.
// Usa time.Now de propósito, e não o s.agora injetável: aquele existe para tornar os
// timestamps do CSV determinísticos e nos testes é uma CONSTANTE — com ele, o intervalo
// sem progresso seria sempre zero e o watchdog nunca dispararia.
func etapaComProgresso(pai context.Context, rotulo, dir string, semProgresso, teto time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(pai, teto)
	defer cancel()

	parado := make(chan struct{})
	travou := make(chan struct{})
	go vigiarProgresso(ctx, dir, semProgresso, cancel, parado, travou)

	err := fn(ctx)
	close(parado)

	if err != nil {
		select {
		case <-travou:
			return &erroPrazo{fmt.Sprintf(
				"%s ficou %s sem baixar nada e foi interrompido — a rede travou; tente de novo",
				rotulo, formatarPrazo(semProgresso))}
		default:
		}
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return &erroPrazo{fmt.Sprintf("%s passou de %s e foi interrompido; tente de novo",
				rotulo, formatarPrazo(teto))}
		}
	}
	return err
}

// vigiarProgresso amostra o tamanho da pasta e cancela se ele parar de crescer. Fecha
// `travou` ANTES de cancelar, para o chamador saber distinguir "travou" de "deu erro".
func vigiarProgresso(ctx context.Context, dir string, semProgresso time.Duration, cancel context.CancelFunc, parado chan struct{}, travou chan struct{}) {
	intervalo := semProgresso / 10
	if intervalo < 50*time.Millisecond {
		intervalo = 50 * time.Millisecond
	}
	tic := time.NewTicker(intervalo)
	defer tic.Stop()

	ultimoTamanho := tamanhoPasta(dir)
	ultimoAvanco := time.Now()
	for {
		select {
		case <-parado:
			return
		case <-ctx.Done():
			return
		case <-tic.C:
			t := tamanhoPasta(dir)
			if t > ultimoTamanho {
				ultimoTamanho, ultimoAvanco = t, time.Now()
				continue
			}
			if time.Since(ultimoAvanco) >= semProgresso {
				close(travou)
				cancel()
				return
			}
		}
	}
}

// tamanhoPasta soma os arquivos de dir (um nível: é onde o yt-dlp escreve). Erro vira 0 —
// pasta ainda inexistente no começo do download é normal, não é falta de progresso.
func tamanhoPasta(dir string) int64 {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entradas {
		if info, err := e.Info(); err == nil && !info.IsDir() {
			total += info.Size()
		}
	}
	return total
}
