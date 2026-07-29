package servidor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// Retomada de um pedido já processado (cmd/servidor -retomar <id>).
//
// Para que serve: iterar em render e em tela custava o ciclo inteiro a cada tentativa —
// ~40 s de seleção mais ~86 s de download, para depois olhar 3 s de render. Retomando, o
// pedido reaparece direto em "aguardando aprovação" com os candidatos que já estavam em
// disco, e o vídeo é reaproveitado (ver videoUsavel). O ciclo de teste cai para o tempo do
// render.
//
// POR QUE É FLAG EXPLÍCITA, E NÃO CARREGAMENTO AUTOMÁTICO
//
// A spec-06 depende de o servidor NÃO carregar estado do disco: é isso que faz um pedido
// travado por crash desaparecer no restart e o material bruto dele voltar a ser limpável
// (autocura). Carregar tudo automaticamente transformaria pedido travado em vazamento
// permanente de disco.
//
// Com uma flag, quem retoma é uma pessoa dizendo qual pedido quer de volta. O mapa continua
// nascendo vazio, a autocura continua valendo, e o teste que a protege
// (TestReinicioLiberaPedidoOrfao) continua verdadeiro.

// Retomar registra na memória do servidor um pedido que já existe em disco, no estado
// "aguardando aprovação". Devolve erro se faltar o essencial — melhor falhar na subida, com
// mensagem clara, que subir e o operador encontrar uma tela vazia.
func (s *Servidor) Retomar(id string) error {
	if id == "" {
		return fmt.Errorf("id vazio")
	}
	dir := filepath.Join(s.baseDir, id)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("pedido %q não existe em %s", id, s.baseDir)
	}

	ped, err := pipeline.Carregar(s.baseDir, id)
	if err != nil {
		// Pedidos anteriores à gravação de metadados não têm pedido.json. Reconstrói o
		// mínimo a partir do legenda.info.json, que o download já deixa em disco: sem isso,
		// todo material já baixado ficaria fora do alcance da retomada — inclusive um vídeo
		// de 900 MB que serve de material de teste.
		ped, err = reconstruirPedido(dir, id)
		if err != nil {
			return fmt.Errorf("pedido %q não tem pedido.json nem dá para reconstruir: %w", id, err)
		}
		fmt.Fprintf(os.Stderr, "aviso: pedido %s sem pedido.json; reconstruído do legenda.info.json "+
			"(a janela da pregação não é recuperável, então o ajuste manual fica sem clamp)\n", id)
		if err := ped.Salvar(s.baseDir); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: não gravei o pedido.json reconstruído: %v\n", err)
		}
	}

	cands, err := candidatosDoDisco(dir)
	if err != nil {
		return fmt.Errorf("pedido %q sem candidatos validados: %w", id, err)
	}
	if len(cands) == 0 {
		return fmt.Errorf("pedido %q tem candidatos.corrigido.json vazio: nada a revisar", id)
	}

	// O texto falado é reconstruído da transcrição, como na fase leve — não vem de cache, para
	// não divergir do que a revisão mostraria num pedido novo.
	transc := filepath.Join(dir, "transcricao.txt")
	textos := textosFalados(transc, cands)

	ped.Status = pipeline.EstadoAguardandoAprovacao
	ped.Erro = ""

	s.mu.Lock()
	s.pedidos[id] = &registro{
		ped:    ped,
		cands:  cands,
		textos: textos,
		// Métricas novas: o pedido retomado mede o ciclo DESTA execução, não o da original.
		// Somar os dois falsearia o CSV de desempenho.
		metricas: &Metricas{ID: id, DuracaoSermaoS: duracaoJanelaS(ped.Inicio, ped.Fim)},
	}
	s.mu.Unlock()

	temVideo := videoUsavel(filepath.Join(dir, "video.mp4"))
	s.logTempos(fmt.Sprintf("retomado %s: %d candidato(s), vídeo em disco: %v", id, len(cands), temVideo))
	return nil
}

// candidatosDoDisco lê o candidatos.corrigido.json — a fonte de verdade validada (spec-09).
// Mesmo formato que salvarCandidatos grava e que o cmd/render consome.
func candidatosDoDisco(dir string) ([]validacao.Candidato, error) {
	b, err := os.ReadFile(filepath.Join(dir, "candidatos.corrigido.json"))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Candidatos []validacao.Candidato `json:"candidatos"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return doc.Candidatos, nil
}

// reconstruirPedido monta o mínimo para retomar um pedido antigo: id e URL. O que NÃO se
// recupera é a janela da pregação (Inicio/Fim), que o operador informou no formulário e nunca
// foi persistida — daí o aviso, porque sem ela o ajuste manual perde o clamp e o CSV de
// desempenho perde a duração do sermão. É degradação anunciada, não silenciosa.
func reconstruirPedido(dir, id string) (*pipeline.Pedido, error) {
	b, err := os.ReadFile(filepath.Join(dir, "legenda.info.json"))
	if err != nil {
		return nil, fmt.Errorf("sem legenda.info.json para reconstruir: %w", err)
	}
	var info struct {
		URL    string `json:"webpage_url"`
		Titulo string `json:"title"`
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return nil, err
	}
	if info.URL == "" {
		return nil, fmt.Errorf("legenda.info.json sem webpage_url")
	}
	// Inicio = 00:00:00 é o LIMITE INFERIOR do clamp do ajuste manual, não a origem do
	// arquivo. A janela da pregação não é recuperável daqui, e 00:00:00 é o clamp mais
	// permissivo possível — degradação anunciada no aviso de quem chama.
	//
	// Antes este 00:00:00 fazia dois trabalhos, e o segundo era uma mentira útil: o cmd/render
	// usava ped.Inicio como origem de tempo do arquivo, então escrever 00:00:00 era o jeito de
	// dizer "o vídeo é o inteiro". Agora a origem é um fato declarado à parte (origem_ms), e
	// este pedido reconstruído NÃO a declara de propósito: quem escreveu o video.mp4 desta
	// pasta pode ter sido o servidor (inteiro, origem 0) ou o cmd/baixar (janela, origem =
	// início), e daqui não há como saber. O render falha dizendo o que falta, em vez de cortar
	// a cena errada com a duração certa.
	return &pipeline.Pedido{ID: id, YouTubeURL: info.URL, Titulo: info.Titulo, Inicio: "00:00:00"}, nil
}
