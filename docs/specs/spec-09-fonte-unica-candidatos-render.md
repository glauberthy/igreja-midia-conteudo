# Spec 09 — Fonte única de verdade dos candidatos no render

## Objetivo

Corrigir a origem dos candidatos que o `cmd/render` transforma em vídeo. Hoje o render
usa a cópia embutida em `pedido.json` (seleção antiga, da chamada única, pré-spec-07),
ignorando o `candidatos.corrigido.json` validado — e até a flag `-cand` explícita.
Resultado: os Shorts saem dos candidatos velhos (2–14 s), nunca dos validados (30–58 s),
violando a regra inviolável nº 3 (nada chega ao humano sem passar pelo validador).

Esta spec estabelece **uma única fonte de verdade**: os candidatos que viram vídeo vêm
SEMPRE do arquivo de seleção validado, nunca de uma cópia embutida no pedido.

## Contexto (diagnóstico já confirmado)

Duas análises independentes (Claude e Claude Code) confirmaram:
- `cmd/render/main.go:46` — `if len(ped.Candidatos) == 0` só busca o arquivo de
  candidatos quando o pedido NÃO traz candidatos embutidos. Como o `pedido.json` tem 5
  candidatos antigos, o bloco é pulado; o arquivo validado e a flag `-cand` são ignorados.
- `pedido.json` foi gravado pela seleção de chamada única (`internal/pipeline/selecao.go`,
  removida na spec-07). O harness novo grava em `candidatos.corrigido.json` e não toca no
  pedido. O `pedido.json` ficou "congelado" no mundo pré-spec-07.
- `ped.Salvar` ao final regrava os candidatos antigos → laço auto-reforçante.
- Fere a regra inviolável nº 3: o embutido é material pré-validação.

## Decisão (Visão 1 — não reabrir)

**O `candidatos.corrigido.json` (saída do harness + validador) é a ÚNICA fonte de
verdade dos candidatos para o render.** O `pedido.json` guarda apenas os dados do pedido
(id, url, início, fim, status) — NUNCA candidatos. Isso elimina a duplicação na raiz:
não há duas fontes a reconciliar, há uma.

## Escopo

Dentro:
- `cmd/render`: ler candidatos SEMPRE do arquivo de seleção. Precedência: (1) flag
  `-cand` se passada; (2) senão, o padrão `trabalho/<id>/candidatos.corrigido.json`.
  Remover a lógica que usa `ped.Candidatos` embutidos.
- Remover o campo `Candidatos` de `pedido.json` (ou parar de gravá-lo e de lê-lo), de
  modo que o pedido não seja mais um portador de candidatos. Confirmar que nada mais no
  sistema depende de `ped.Candidatos` (o `internal/pipeline/selecao.go` que o gravava já
  foi removido na spec-07 — verificar se sobrou consumidor).
- `ped.Salvar` no render: não deve regravar candidatos (já que o campo sai). Reavaliar
  se o render precisa mesmo persistir o pedido; se só atualiza `status`, manter apenas
  isso.
- Log no início do render: qual arquivo de candidatos foi lido e quantos candidatos
  encontrou (ex.: "render: lendo trabalho/sermao/candidatos.corrigido.json, 4 candidatos").

Fora:
- Lógica de seleção/validação (não muda).
- Formato do `candidatos.corrigido.json` (não muda).

## Contrato

- `render -id <id>` lê `trabalho/<id>/candidatos.corrigido.json` e gera um vídeo por
  candidato.
- `render -id <id> -cand <arquivo>` lê do arquivo indicado (a flag explícita SEMPRE
  vence).
- Se o arquivo de candidatos não existir ou estiver vazio: erro claro ("nenhum candidato
  validado encontrado em <caminho>; rode a seleção antes"), sem cair em fonte alternativa.
- O `pedido.json` não carrega candidatos. Se um `pedido.json` antigo ainda tiver o campo,
  o render o ignora (não usa como fonte).

## Critérios de aceite

- [ ] Com `candidatos.corrigido.json` contendo 4 candidatos de 30–58 s, `render -id sermao`
      gera exatamente 4 vídeos de 30–58 s (não 5 de 2–14 s).
- [ ] `-cand <arquivo>` explícito é sempre respeitado, mesmo que exista `pedido.json` com
      candidatos antigos.
- [ ] O render não lê nem regrava candidatos no `pedido.json`.
- [ ] Um `pedido.json` legado com candidatos embutidos é ignorado como fonte (não
      "sombreia" o arquivo validado).
- [ ] Log mostra o arquivo lido e a contagem de candidatos.
- [ ] Erro claro quando não há candidatos validados (não renderiza material não-validado).
- [ ] Teste: arquivo validado vence candidatos embutidos no pedido; `-cand` vence o
      padrão; ausência de arquivo → erro, não fallback.
- [ ] `go build ./...` e `go test ./...` verdes.
- [ ] Regra inviolável nº 3 honrada: o render só emite candidatos que passaram pelo
      validador.

## Como validar

```bash
go test ./...
# limpar vídeos antigos e renderizar a partir do validado:
rm -f finalizados/sermao/short_*.mp4
go run ./cmd/render -id sermao
# conferir contagem e durações reais:
for f in finalizados/sermao/short_*.mp4; do
  echo -n "$f: "; ffprobe -v error -show_entries format=duration \
    -of default=noprint_wrappers=1:nokey=1 "$f"
done
# esperado: 4 vídeos, todos 30–58 s.
```

## Segunda ocorrência da mesma classe: a ORIGEM DE TEMPO do vídeo (2026-07-29)

Esta spec nasceu de um dado com **duas fontes** (candidatos no `pedido.json` e no arquivo
validado). Meses depois a mesma classe reapareceu num dado com **nenhuma fonte** — a origem
de tempo do `video.mp4` — e o efeito foi pior, porque não havia arquivo errado para culpar:
havia uma **suposição**.

O `cmd/render` usava `ped.Inicio` como o instante do vídeo original a que o t=0 do arquivo
corresponde. Verdade para quem baixa por janela (`cmd/baixar`, `--download-sections`); falso
para o servidor, que baixa o **vídeo inteiro** e grava um `Inicio` real (o início da
pregação). Os dois contratos se cruzavam no mesmo `pedido.json`, e o render não tinha como
saber qual valia. `cmd/render -id <pedido do servidor>` gerava Shorts **deslocados pelo
Inicio** (49 min, no caso real) com a **duração correta**.

**Correção — mesma receita desta spec, aplicada ao contrário.** Ali o problema era duas
fontes e a solução foi eleger uma; aqui era nenhuma fonte e a solução foi **criar o fato e
declará-lo**: `pipeline.Pedido.OrigemMs` (`origem_ms` no JSON). Quem **escreve** o vídeo
declara onde ele começa — `cmd/baixar` declara `inicio`, `BaixarVideoCompleto` declara `0` —
e o render **lê**, nunca deduz.

Quatro decisões que valem para qualquer repetição:

- **O escritor DEVOLVE o fato; quem chama guarda.** `BaixarVideoCompleto` e
  `baixarVideoJanela` retornam a origem em vez de escrevê-la no `Pedido`. Motivo: o servidor
  entrega uma **cópia** do pedido às dependências (`copiaPedido`), e atribuição em cópia se
  perde **em silêncio** — sem erro de compilação, sem aviso. Retorno ignorado aparece na linha
  de quem ignora; mutação descartada não aparece em lugar nenhum. Isso também eliminou uma
  duplicação que havia surgido no primeiro remendo: o download declarava (inerte, na cópia) e o
  servidor reafirmava — dois lugares dizendo "vídeo inteiro → origem 0", e dois lugares que
  afirmam a mesma coisa divergem.

- **Ponteiro, não `int`.** Zero é valor legítimo (vídeo inteiro) e tem de ser distinguível de
  "ninguém declarou". Confundir os dois é a origem do bug.
- **Sem padrão.** Origem ausente (pedidos anteriores) **falha** com mensagem que diz o que
  fazer. Assumir silenciosamente foi como o bug nasceu; assumir de novo no remendo o
  reintroduziria.
- **Não deduzir pela duração.** Uma janela de 35 min e um vídeo inteiro de 35 min são
  indistinguíveis. Onde há como declarar, declarar.

**E o teste tem de olhar o CONTEÚDO.** A duração saía certa nos dois casos — foi ela que
deixou o bug passar (quatro Shorts com 37/48/46/30 s, os números esperados, cena errada em
todos). O teste de regressão
(`internal/video/origem_do_video_test.go`) gera uma fonte sintética em que o canal R codifica
o instante (`R = 2·T`), renderiza pelo caminho da CLI um pedido no formato do servidor e
compara o **pixel** do frame. Verificado por mutação: reintroduzindo `ped.Inicio`, o teste
falha e nomeia a causa.

### Ligação com o cache de vídeo por ID — DESENHADO na spec-05 v3

O cache por vídeo (`videos/<video_id>/`) deixou de ser intenção: está desenhado na **spec-05
v3** (2026-07-29), para dois pedidos do mesmo culto não baixarem 570 MB duas vezes.

A regra que a spec-05 v3 fixou, e que vem direto desta spec:

> **Cada arquivo de vídeo carrega a própria declaração de origem, ao lado dele.**
> `videos/<id>/video.json` descreve `videos/<id>/video.mp4`;
> `pedido.json.origem_ms` descreve `trabalho/<id>/video.mp4`.

E um único resolvedor, `videocache.Localizar(videosDir, baseDir, ped) (path, origemMs, err)`,
como o **único** lugar que decide qual arquivo e qual origem: vídeo na pasta do pedido vence
(fluxo por janela, mais específico), senão o cache, senão erro claro. É a mesma lição do
"eleja uma fonte" desta spec, agora aplicada ao par arquivo+origem em vez de aos candidatos.

No cache **os arquivos serão sempre o vídeo INTEIRO, com origem 0** — a ambiguidade desaparece
estruturalmente ali, porque não existe variante "janela" no cache. Duas consequências a
respeitar:

1. **Não recrie a suposição.** É tentador dizer "no cache é sempre 0, então nem preciso do
   campo". O campo continua sendo a única forma de o render saber a origem de um arquivo que
   ele não baixou — e o `cmd/baixar` por janela continua existindo, gravando na pasta do
   pedido. Enquanto os dois caminhos coexistirem, a origem tem de ser declarada.
   Como o escritor **devolve** a origem, quem muda é só o chamador: o baixador fica intacto
   quando o destino do fato passar de `pedido.json` para `videos/<id>/video.json`.
2. **A declaração acompanha o arquivo.** Como o vídeo do cache é compartilhado entre pedidos,
   a origem é propriedade do **arquivo**, não do pedido: fica em `videos/<video_id>/video.json`
   ao lado do `.mp4`, e o `pedido.json` só aponta para o vídeo (campo `video_id`). Para o vídeo
   que vive dentro de `trabalho/<id>/` (`cmd/baixar` por janela), `pedido.json.origem_ms`
   continua sendo o lugar certo. **Dois arquivos, duas declarações, uma regra** — e um
   resolvedor só.
3. **O `video_id` vira nome de diretório, então precisa ser validado.** Hoje ele só alimenta
   um iframe; no cache ele escolhe onde escrevemos. Sem `^[A-Za-z0-9_-]{11}$`, uma URL
   hostil decide o caminho. É a mesma preocupação do `retencao.caminhoSeguro`, num lugar novo.

## Nota

Este bug era um fóssil da arquitetura pré-spec-07: a seleção de chamada única gravava
candidatos no pedido; ao trocá-la pelo harness (que grava no arquivo validado), o render
continuou lendo a fonte antiga. Lição de contrato entre etapas: ao mudar quem PRODUZ um
dado, verificar quem o CONSOME. Testes de unidade de cada peça passavam; só o teste de
ponta a ponta com dados reais expôs a desconexão.
