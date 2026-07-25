# Spec 16 — Auditoria de fidelidade dos candidatos (cmd/auditar)

> **Spec retroativa.** Documenta código já implementado e validado no commit `3c4b949`
> (`cmd/auditar: auditoria determinística dos candidatos contra a legenda`), com a grade
> de critérios adicionada depois. Escrita depois do fato para acertar a dívida documental.

## Objetivo

Uma ferramenta **determinística** (sem LLM) que cruza os candidatos já validados de um
pedido com a legenda real e acusa defeitos de **fidelidade** e de **corte** que a seleção
(que é julgamento do modelo) não garante sozinha. É a rede de verificação que responde
"o trecho escolhido corresponde ao que foi realmente falado, e o corte está bem-formado?".

## Contexto

A seleção (harness) é julgamento do modelo; a validação (Fase 5) é uma rede determinística
interna. Faltava uma ferramenta **externa e sob demanda** para o operador/desenvolvedor
conferir os candidatos contra a transcrição a qualquer momento — inclusive os já gravados.

Ela nasceu de uma pergunta do dono ("você cruzou os resultados com a legenda, ou só recebeu
o resultado do modelo?") e, na primeira execução, expôs três coisas reais: o bug do "hook
clipado" da Fase 5 (start deslizado ~3 s — corrigido no commit `024e440`), um pedido com
artefatos de fontes misturadas, e um erro de ASR do YouTube que invertia o sentido ("sem
Cristo" por "em Cristo"). Ver `resultados/auditoria-fidelidade.md`.

## Escopo

Dentro:
- `cmd/auditar`: lê `trabalho/<id>/candidatos.corrigido.json` + `transcricao.txt`, cruza
  cada candidato com as frases da legenda (`harness.Frasear`) e reporta em markdown.
- Invariantes verificadas por candidato:
  1. **hook existe na legenda** e **começa exatamente no `start`** (acusa "hook CLIPADO"
     se o hook começa antes do start, ou "start com sobra" se começa depois; "hook não
     encontrado / inventado" se não casa);
  2. o **`end` cai no fim de uma frase completa** (não corta fala no meio);
  3. **duração** em 30–60 s.
- Flags de leitura: `-texto` (imprime o texto realmente falado na janela, insumo da
  revisão humana) e `-criterios` (grade dos 5 critérios da Fase 4 — por que o score é
  aquele).

Fora:
- **Não** usa LLM e **não** julga doutrina — a certificação teológica continua humana
  (regra inviolável nº 6). O `-texto`/`-criterios` são insumo para essa revisão.
- Não corrige nada: só reporta (a correção de bugs que ela revela é feita à parte).

## Contrato e uso

```bash
go run ./cmd/auditar -id <pedido>            # audita um pedido
go run ./cmd/auditar -todos                  # todos os pedidos com candidatos em trabalho/
go run ./cmd/auditar -id <pedido> -texto     # inclui o texto falado de cada trecho
go run ./cmd/auditar -id <pedido> -criterios # grade de critérios da Fase 4
go run ./cmd/auditar -todos > resultados/auditoria.md   # salvar o relatório
```

Flags: `-id`, `-todos`, `-base` (padrão `trabalho`), `-texto`, `-criterios`.

Saída: markdown por pedido — `## <id> — N candidato(s)` e, por candidato, `status`
(✅ fiel ou ⚠ com a lista de problemas), o hook e, opcionalmente, o texto falado e a grade.
**Código de saída ≠ 0** se encontrar algum problema (útil em verificação automatizada).

## Critérios de aceite

- [x] Determinístico, sem LLM; lê os artefatos do pedido em `trabalho/<id>/`.
- [x] Acusa hook clipado, hook inventado, end fora de fim de frase e duração fora de 30–60 s.
- [x] `-texto` imprime o texto falado; `-criterios` imprime a grade da Fase 4.
- [x] Sai com código ≠ 0 quando há problema.
- [x] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./cmd/auditar/    # TestAuditarCandidato* (fiel, hook clipado, hook inventado,
                          # end no meio da fala, duração fora) + TestGradeCriterios
go run ./cmd/auditar -todos
```

## Quando rodar

- **Depois de uma seleção, antes de renderizar/entregar** — pega corte mal-formado antes
  de virar vídeo.
- Ao **investigar uma suspeita** (trecho estranho, legenda esquisita): `-texto` mostra o
  que foi realmente falado.
- Em **verificação automatizada** (o código de saída ≠ 0 sinaliza problema).
- Ao mexer na Fase 3/5 (delimitação/validação): é o teste de fumaça contra dados reais.
