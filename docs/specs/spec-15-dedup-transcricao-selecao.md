# Spec 15 — Desduplicação da transcrição nas fases de modelo

> **Spec retroativa.** Documenta código já implementado e validado no commit `ae27fc8`
> (`harness: desduplica a transcrição nas fases de modelo (~68% menos tokens)`). Escrita
> depois do fato para acertar a dívida documental exposta no inventário de specs. Nenhum
> código novo — só documentação.

## Objetivo

Alimentar as fases de MODELO da seleção (Fase 1 e Fase 2) com uma transcrição
**desduplicada** — uma linha por frase — em vez da transcrição bruta do YouTube, que vem
inflada pela repetição das legendas "rolling". Menos tokens, menos ruído, mais estável.

## Contexto

A legenda automática do YouTube é "rolling": cada bloco repete o texto que continua na
tela, então a transcrição bruta traz cada frase 2–3 vezes. As Fases 1 e 2 recebiam essa
transcrição inteira no prompt, o que:

- **desperdiçava contexto** (a transcrição de um sermão chegava a ~25k tokens, a maior
  parte repetição); e
- **induzia o modelo a repetir/loopar na saída** — texto repetido no input puxa
  repetição no output. Em modelos com quantização agressiva (Qwen Q3), isso estourava o
  `max_tokens` da Fase 2 e truncava o JSON (ver spec-08). Ver também
  `docs/otimizacao-modelo-local.md`.

O harness já tinha a lógica de desduplicação — `harness.Frasear`, usada pela Fase 3 e pela
legenda queimada (spec-07/spec-12). O que faltava era reusá-la como **entrada do modelo**.

## Escopo

Dentro:
- Uma função que rende a transcrição desduplicada como texto `[HH:MM:SS] frase`, uma
  frase por linha, reusando `Frasear`.
- Ligar essa versão nas Fases **1 e 2** (as que falam com o modelo), tanto no `Selecionar`
  (produção) quanto no `cmd/harness` (diagnóstico).

Fora:
- A **Fase 3** NÃO muda: continua sobre a transcrição **bruta** (ver "Decisões").
- Não altera prompts, sampling nem a lógica das fases — só o texto que a Fase 1/2 recebem.
- Não mexe nas palavras do pregador (regra inviolável nº 2): `Frasear` só remove a
  repetição de rolagem e reconstrói o fluxo linear; o texto das frases é idêntico.

## Decisões já tomadas (não reabrir)

- **Fase 3 continua na transcrição bruta.** A Fase 3 delimita o tempo do corte e precisa
  dos timestamps **por palavra**: numa frase longa, cada palavra tem o seu segundo (o que
  dá `FimMs > InicioMs`). Na forma linear, a frase inteira carrega **um só** timestamp
  (o de início), o que colapsaria `FimMs` para `InicioMs` e quebraria a duração. Por isso
  o linear é só para o MODELO entender/escolher; o tempo exato sai da bruta.
- **A âncora casa nas duas.** A frase-âncora que a Fase 2 escolhe vem das MESMAS frases
  que a Fase 3 encontra na bruta (ambas via `Frasear`), então ela sempre casa — esta é a
  invariante crítica da mudança.
- Reusar `Frasear` (não reimplementar dedup) — fonte única da desduplicação.

## Contrato e implementação

`internal/harness/fase3.go`:

```
// uma linha por frase, "[HH:MM:SS] texto", reusando Frasear
func TranscricaoLinear(transcricao string) string
```

- `internal/harness/orquestra.go` (`Selecionar`): calcula `transcLinear` uma vez; passa
  para `Fase1Mapa` e `Fase2Candidatos`; a `Fase3Delimitar` recebe a transcrição **bruta**.
- `cmd/harness/main.go`: espelha — Fases 1/2 no linear, Fase 3 na bruta.

## Critérios de aceite

- [x] `TranscricaoLinear` reusa `Frasear`, uma linha por frase `[HH:MM:SS] texto`.
- [x] Fases 1 e 2 recebem o linear; Fase 3 recebe a bruta (em `Selecionar` e `cmd/harness`).
- [x] Nenhuma palavra do pregador é alterada (só remove a repetição de rolagem).
- [x] Invariante testada: a âncora vinda do texto linear casa na Fase 3 sobre a bruta.
- [x] `go build ./...` e `go test ./...` verdes.
- [x] Ganho de tokens medido numa transcrição real: **25.246 → 8.051 tokens (−68%)**.

## Como validar

```bash
go test ./internal/harness/    # TestTranscricaoLinearDesduplicaEPreserva
                               # TestTranscricaoLinearAncoraCasaNaFase3Bruta
```

- `TestTranscricaoLinearDesduplicaEPreserva`: alimenta legenda rolling crua e confere que
  a saída é uma linha por frase, no formato certo, com as palavras preservadas e sem a
  duplicação (ex.: "possível" aparece uma vez), e que fica menor que a bruta.
- `TestTranscricaoLinearAncoraCasaNaFase3Bruta`: pega a 1ª frase do texto **linear** como
  âncora e roda `Fase3Delimitar` sobre a transcrição **bruta** — tem que delimitar (as
  frases são as mesmas). É a prova da invariante crítica.

## Nota

O dedup remove um **agravante plausível** do loop/truncamento da Fase 2 com o Qwen Q3
(input repetitivo puxa output repetitivo), **mas isso não foi verificado**: o Qwen Q3 não
foi re-executado depois do dedup — a validação do dedup foi feita com o Gemma. O ganho
vale para **qualquer** modelo por outro motivo, esse sim medido: menos tokens = mais
rápido e mais barato (−68% de input). A folga de contexto que ele abriu também embasou a
redução do `-c` do llama-server de 64k para 24k (`docs/otimizacao-modelo-local.md`).
