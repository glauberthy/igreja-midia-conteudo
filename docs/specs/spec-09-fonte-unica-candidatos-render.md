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

## Nota

Este bug era um fóssil da arquitetura pré-spec-07: a seleção de chamada única gravava
candidatos no pedido; ao trocá-la pelo harness (que grava no arquivo validado), o render
continuou lendo a fonte antiga. Lição de contrato entre etapas: ao mudar quem PRODUZ um
dado, verificar quem o CONSOME. Testes de unidade de cada peça passavam; só o teste de
ponta a ponta com dados reais expôs a desconexão.
