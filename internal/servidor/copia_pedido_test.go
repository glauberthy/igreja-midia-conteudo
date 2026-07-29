package servidor

import (
	"reflect"
	"testing"

	"srtclean/internal/pipeline"
)

// Este arquivo guarda o PADRÃO, não um caso: mutação através de copiaPedido.
//
// copiaPedido entrega `c := *reg.ped` às dependências, para que uma goroutine não escreva no
// registro compartilhado com os handlers. O efeito colateral é que qualquer campo que a
// dependência preencha na cópia é DESCARTADO em silêncio — sem erro de compilação, sem aviso.
// Foi assim que a origem de tempo do vídeo virou bug (spec-09), e é por isso que existe o
// aplicarTitulo.

// TestCopiaPedidoRasaEArmadilhaDoPonteiro documenta e trava as duas metades do risco de cópia
// rasa no único campo de referência que o Pedido tem hoje (OrigemMs *int).
func TestCopiaPedidoRasaEArmadilhaDoPonteiro(t *testing.T) {
	s := &Servidor{}
	original := &pipeline.Pedido{ID: "p1"}
	original.DeclararOrigem(1000)
	reg := &registro{ped: original}

	copia := s.copiaPedido(reg)

	// (1) A cópia é RASA: os dois apontam para o MESMO int. Não é um defeito a corrigir, é o
	// fato que torna a armadilha possível — se um dia alguém escrever `*copia.OrigemMs = x`,
	// o valor vaza para o original. O teste afirma isso para que a mudança de comportamento
	// (cópia profunda) seja uma decisão, não um acidente.
	if copia.OrigemMs != original.OrigemMs {
		t.Log("copiaPedido passou a copiar OrigemMs em profundidade — se foi de propósito, " +
			"atualize o comentário de copiaPedido; a regra do retorno pode ser relaxada")
	}

	// (2) DeclararOrigem SUBSTITUI o ponteiro, então declarar na cópia NÃO afeta o original.
	// É esta metade que descartava a origem em silêncio no caminho do servidor.
	copia.DeclararOrigem(9999)
	if o, err := original.Origem(); err != nil || o != 1000 {
		t.Errorf("declarar na cópia alterou o original (origem=%d, err=%v): DeclararOrigem "+
			"deve substituir o ponteiro, nunca escrever através dele", o, err)
	}
	if o, _ := copia.Origem(); o != 9999 {
		t.Errorf("a cópia não recebeu a nova origem: %d", o)
	}
}

// TestPedidoSoTemUmCampoDeReferencia é o alarme para o futuro: se alguém acrescentar slice,
// map, ponteiro ou função ao Pedido, a cópia rasa passa a perder parte das mutações e vazar
// outra parte — e isso não se descobre sem olhar campo a campo. O teste falha na adição,
// obrigando a decisão explícita (copiar em profundidade, ou o campo sai por retorno).
func TestPedidoSoTemUmCampoDeReferencia(t *testing.T) {
	tipo := reflect.TypeOf(pipeline.Pedido{})
	var referencia []string
	for i := 0; i < tipo.NumField(); i++ {
		f := tipo.Field(i)
		switch f.Type.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
			referencia = append(referencia, f.Name+" "+f.Type.String())
		}
	}
	// Conhecido e registrado: OrigemMs *int, seguro porque DeclararOrigem troca o ponteiro.
	esperado := []string{"OrigemMs *int"}
	if !reflect.DeepEqual(referencia, esperado) {
		t.Errorf("os campos de referência do Pedido mudaram.\n  agora:    %v\n  esperado: %v\n\n"+
			"copiaPedido faz cópia RASA (c := *reg.ped). Com campo de referência novo, parte das "+
			"mutações da dependência se perde e parte VAZA para o registro compartilhado. Decida: "+
			"copiar em profundidade, ou o dado sai por RETORNO (foi o que resolveu a origem do "+
			"vídeo — ver spec-09). Depois atualize este teste e o comentário de copiaPedido.",
			referencia, esperado)
	}
}

// TestTituloDoBaixadorChegaAoPedidoNaFaseLeve cobre o PONTO DE USO do copy-back, não a
// função: exercita a fase leve inteira com um baixador que escreve o título na cópia (como o
// real faz a partir do legenda.info.json) e verifica que o título chegou ao pedido.
//
// Existe porque a auditoria do padrão encontrou o buraco: com só o teste da função
// (TestAplicarTituloVoltaParaOOriginal), apagar a chamada `s.aplicarTitulo(reg, copia)` da
// faseLeve deixava a suíte INTEIRA verde — e o título desaparecia do pedido.json e do log de
// rodadas sem ninguém notar. É a mesma classe da origem do vídeo: mutação em cópia que se
// perde em silêncio.
func TestTituloDoBaixadorChegaAoPedidoNaFaseLeve(t *testing.T) {
	const titulo = "Pr. Fulano | 19/07/26 | Culto Matinal"
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	// Troca o baixador da fase leve por um que descobre o título (como o .info.json real).
	s.baixador = &baixadorFake{transc: "[00:00:00] a graça basta.", base: s.baseDir, titulo: titulo}

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	s.mu.Lock()
	got := s.pedidos["teste-1"].ped.Titulo
	s.mu.Unlock()
	if got != titulo {
		t.Errorf("o título não chegou ao pedido: %q, quero %q.\n"+
			"O baixador escreve na CÓPIA (copiaPedido); sem o aplicarTitulo na faseLeve o valor "+
			"morre ali — e o pedido.json e o log de rodadas saem sem título.", got, titulo)
	}
}

// TestAplicarTituloVoltaParaOOriginal prova que o único copy-back existente funciona. Sem
// isto, o título que o baixador descobre no .info.json seria mais uma mutação perdida — e o
// pedido apareceria sem título na tela do operador.
func TestAplicarTituloVoltaParaOOriginal(t *testing.T) {
	s := &Servidor{}
	original := &pipeline.Pedido{ID: "p1"}
	reg := &registro{ped: original}

	copia := s.copiaPedido(reg)
	copia.Titulo = "Culto de Domingo — 19/07" // é o que o BaixarLegenda faz
	copia.Status = pipeline.EstadoErro        // e isto o servidor descarta de propósito

	if original.Titulo != "" {
		t.Fatal("a cópia não é cópia: escrever nela já afetou o original")
	}
	s.aplicarTitulo(reg, copia)

	if original.Titulo != "Culto de Domingo — 19/07" {
		t.Errorf("o título não voltou da cópia: %q", original.Titulo)
	}
	// E o Status NÃO volta: o servidor é o dono do status, e o decide a partir do erro
	// devolvido. Se um dia voltar, um download que falha e recupera deixaria o pedido em
	// "erro" para sempre.
	if original.Status == pipeline.EstadoErro {
		t.Error("o Status da cópia voltou para o original: o servidor é o dono do status")
	}
}
