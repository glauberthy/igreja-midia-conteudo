package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeSequencia devolve uma resposta (e opcionalmente um erro) por chamada, em ordem —
// para simular "1ª inválida, 2ª válida" etc. Conta quantas vezes foi chamado.
type fakeSequencia struct {
	respostas []string
	erros     []error
	chamadas  int
}

func (f *fakeSequencia) Completar(ctx context.Context, sistema, usuario string, maxTokens int) (string, error) {
	i := f.chamadas
	f.chamadas++
	var resp string
	if i < len(f.respostas) {
		resp = f.respostas[i]
	}
	var err error
	if i < len(f.erros) {
		err = f.erros[i]
	}
	return resp, err
}

// capturaLog troca o LogTentativa por um coletor durante o teste e o restaura ao fim.
func capturaLog(t *testing.T) *[]string {
	t.Helper()
	var logs []string
	original := LogTentativa
	LogTentativa = func(msg string) { logs = append(logs, msg) }
	t.Cleanup(func() { LogTentativa = original })
	return &logs
}

const mapaValido = `{"tema_central":"graça","blocos":[{"assunto":"abertura","inicio_aprox":"00:00:01","fim_aprox":"00:00:40"}]}`

func TestRetryInvalidaDepoisValidaSucedeNa2a(t *testing.T) {
	logs := capturaLog(t)
	fake := &fakeSequencia{respostas: []string{"isto não é json", mapaValido}}

	mapa, err := Fase1Mapa(context.Background(), fake, "P", "t")
	if err != nil {
		t.Fatalf("deveria suceder na 2ª tentativa, mas falhou: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("esperava 2 chamadas ao modelo, veio %d", fake.chamadas)
	}
	if len(*logs) != 1 {
		t.Errorf("esperava 1 log de retry, veio %d: %v", len(*logs), *logs)
	}
	if mapa.TemaCentral != "graça" {
		t.Errorf("mapa final inesperado: %+v", mapa)
	}
}

func TestRetryTresInvalidasFalhaApos3(t *testing.T) {
	logs := capturaLog(t)
	fake := &fakeSequencia{respostas: []string{"lixo 1", "lixo 2", "lixo 3", mapaValido}}

	_, err := Fase1Mapa(context.Background(), fake, "P", "t")
	if err == nil {
		t.Fatal("esperava erro após 3 tentativas inválidas")
	}
	if fake.chamadas != MaxTentativas {
		t.Errorf("esperava %d chamadas (não mais), veio %d", MaxTentativas, fake.chamadas)
	}
	if !strings.Contains(err.Error(), "3 tentativas") {
		t.Errorf("erro deveria citar a contagem de tentativas: %v", err)
	}
	if !strings.Contains(err.Error(), "último motivo") {
		t.Errorf("erro deveria citar o último motivo: %v", err)
	}
	if len(*logs) != MaxTentativas {
		t.Errorf("esperava %d logs de tentativa falha, veio %d: %v", MaxTentativas, len(*logs), *logs)
	}
}

func TestRetryValidaNa1aSemRetry(t *testing.T) {
	logs := capturaLog(t)
	fake := &fakeSequencia{respostas: []string{mapaValido}}

	if _, err := Fase1Mapa(context.Background(), fake, "P", "t"); err != nil {
		t.Fatalf("resposta válida na 1ª não deveria falhar: %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("esperava 1 chamada (sem retry), veio %d", fake.chamadas)
	}
	if len(*logs) != 0 {
		t.Errorf("resposta válida na 1ª não deveria gerar log de retry: %v", *logs)
	}
}

func TestRetryCobreErroDeRede(t *testing.T) {
	// (a) da spec: erro de transporte/rede também merece retry.
	capturaLog(t)
	fake := &fakeSequencia{
		respostas: []string{"", mapaValido},
		erros:     []error{errors.New("connection refused"), nil},
	}
	if _, err := Fase1Mapa(context.Background(), fake, "P", "t"); err != nil {
		t.Fatalf("erro de rede na 1ª deveria ser refeito e suceder na 2ª: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("esperava 2 chamadas, veio %d", fake.chamadas)
	}
}

func TestRetryFase2CamposFaltando(t *testing.T) {
	// (c) da spec: JSON válido mas com campos obrigatórios faltando → retry.
	capturaLog(t)
	mapa := Mapa{TemaCentral: "graça", Blocos: []BlocoEnsino{{Assunto: "a", InicioAprox: "00:00:01", FimAprox: "00:00:40"}}}
	semAncora := `{"candidatos":[{"bloco":"a"}]}`                       // falta frase_ancora
	ok := `{"candidatos":[{"bloco":"a","frase_ancora":"a graça basta"}]}` // completo
	fake := &fakeSequencia{respostas: []string{semAncora, ok}}

	cands, err := Fase2Candidatos(context.Background(), fake, "P", mapa, "t")
	if err != nil {
		t.Fatalf("deveria refazer por campo faltando e suceder na 2ª: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("esperava 2 chamadas, veio %d", fake.chamadas)
	}
	if len(cands) != 1 || cands[0].FraseAncora != "a graça basta" {
		t.Errorf("candidatos inesperados: %+v", cands)
	}
}

func TestRetryFase4CriteriaFaltando(t *testing.T) {
	// Fase 4: falta um dos 5 campos de criteria → retry (formato), não julgamento.
	capturaLog(t)
	semFormat := `{"criteria":{"context_fidelity":28,"pastoral_value":27,"completeness":18,"opening_strength":9},"observacoes":"x"}`
	completo := `{"criteria":{"context_fidelity":28,"pastoral_value":27,"completeness":18,"opening_strength":9,"format_fit":9},"observacoes":"x"}`
	fake := &fakeSequencia{respostas: []string{semFormat, completo}}

	a, err := Fase4Avaliar(context.Background(), fake, "P", "trecho")
	if err != nil {
		t.Fatalf("deveria refazer por criteria incompleto e suceder na 2ª: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("esperava 2 chamadas, veio %d", fake.chamadas)
	}
	if a.Criteria.FormatFit != 9 {
		t.Errorf("avaliação final inesperada: %+v", a)
	}
}

// Nota conceitual (spec-08): retry NÃO cobre conteúdo ruim. Uma avaliação bem-formada
// com notas baixas é VÁLIDA de formato — passa de primeira, sem retry. Quem reprova é
// a combinação/veto (Fase 4) e a validação final (Fase 5), não a rede de retry.
func TestRetryNaoRefazNotaBaixa(t *testing.T) {
	logs := capturaLog(t)
	notaBaixa := `{"criteria":{"context_fidelity":3,"pastoral_value":4,"completeness":2,"opening_strength":1,"format_fit":1},"observacoes":"distorce"}`
	fake := &fakeSequencia{respostas: []string{notaBaixa}}

	if _, err := Fase4Avaliar(context.Background(), fake, "P", "trecho"); err != nil {
		t.Fatalf("nota baixa é formato VÁLIDO, não deveria falhar: %v", err)
	}
	if fake.chamadas != 1 || len(*logs) != 0 {
		t.Errorf("nota baixa não deveria disparar retry: chamadas=%d logs=%v", fake.chamadas, *logs)
	}
}
