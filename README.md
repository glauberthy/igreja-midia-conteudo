# Metodologia de specs

Como escrevemos specs para o Claude Code executar neste projeto. A ideia é
**spec-driven development**: cada incremento é uma spec pequena, com escopo fechado
e critérios de aceite verificáveis. O agente executa a spec ativa, nada além dela.

## Princípios

1. **Uma spec por incremento.** Fatias pequenas e verificáveis. Nunca "faça o
   pipeline inteiro". Prefira "implemente a etapa X, com este contrato, validada por
   estes critérios".
2. **Escopo fechado.** A spec diz explicitamente o que está dentro e o que está fora.
   O que está fora, o agente não faz — vira spec futura.
3. **Decisões já tomadas não se reabrem.** A spec lista o que já foi decidido (no
   spike, no BRD) para o agente não redescobrir nem contradizer.
4. **Critérios de aceite testáveis.** Toda spec termina com uma checklist objetiva e
   os comandos exatos para verificar (build, teste, execução).
5. **Sem segredo, sem palavra do pregador alterada, validador obrigatório.** As
   regras do `CLAUDE.md` valem em toda spec.

## Template

Copie a estrutura abaixo para cada nova spec (`docs/specs/spec-NN-nome.md`):

```
# Spec NN — <título curto>

## Objetivo
Uma frase: o que esta spec entrega.

## Contexto
Por que agora, e de onde vem (BRD RN-xxx, aprendizados do spike). Links.

## Escopo
Dentro: <lista do que será feito>
Fora:   <lista do que NÃO será feito nesta spec; vira spec futura>

## Decisões já tomadas (não reabrir)
<lista das decisões fechadas que restringem a implementação>

## Passos de implementação
Ordenados, pequenos. Cada passo deve ser verificável isoladamente.
1. ...
2. ...

## Contratos e interfaces
Formatos de dados, assinaturas de função, formato de arquivos de entrada/saída.

## Critérios de aceite
Checklist objetiva. Cada item é verdadeiro ou falso, sem ambiguidade.
- [ ] ...
- [ ] `go build ./...` passa
- [ ] `go test ./...` passa

## Como validar
Comandos exatos a rodar e o resultado esperado.

## Fora de escopo / próximos passos
O que fica para a próxima spec.
```

## Dependências de sistema

Além do Go, a fase de produção usa ferramentas externas de linha de comando. Elas
**não** são módulos Go — o projeto apenas as invoca como subprocesso.

- **yt-dlp** (spec-03): baixa a legenda automática e o vídeo do YouTube.
  - Recomendado: `python3 -m pip install -U yt-dlp` (ou `pipx install yt-dlp`).
  - Alternativas: `sudo apt install yt-dlp` (Debian/Ubuntu recentes), `brew install yt-dlp` (macOS),
    ou o binário estático em https://github.com/yt-dlp/yt-dlp/releases.
  - Verifique com `yt-dlp --version`. O caminho do binário é configurável (`-bin`).
- **ffmpeg** (spec-03 e spec-04): o yt-dlp usa o ffmpeg para converter a legenda em
  `.srt` (`--convert-subs`) e para recortar/mesclar o vídeo; a spec-04 o usará direto.
  - `sudo apt install ffmpeg` / `brew install ffmpeg`. Verifique com `ffmpeg -version`.

Sem legenda automática em português, o download **para** com mensagem clara: não há
transcrição local (Whisper etc.). É uma decisão de projeto (BRD DP-001).

## Ordem prevista das specs (roadmap)

Cada spec é executada pelo Code na ordem, uma de cada vez, e aceita pelos seus
critérios antes da seguinte.

- **spec-01 — Fundação** (aceita): projeto Go limpo, testes, `.gitignore`.
- **spec-02 — Orquestração da seleção**: modelo de `Pedido` e fluxo
  srtclean → modelo → validar num pacote reutilizável. Sem vídeo.
- **spec-03 — Download e legenda**: `yt-dlp` encapsulado; baixa o trecho da pregação
  e gera a transcrição. Sem legenda pt → para (DP-001).
- **spec-04 — Vídeo 9:16 e legenda**: `ffmpeg` encapsulado; corta cada candidato,
  reenquadra vertical, queima legenda, grava em `finalizados/`.
- **spec-05 — Servidor e página** (Partes 1 e 2 aceitas; Parte 3 pendente): HTTP em
  porta dedicada, fluxo invertido (selecionar antes de baixar), player e aprovação.
- **spec-06 — Retenção e limpeza**: descarta o vídeo bruto, preserva texto/logs.

Fluxo do operador (não é código): assiste ao culto e identifica início/fim da
pregação → abre a página, informa {link, início, fim} → aguarda → pega os Shorts de
`finalizados/` → envia ao pastor pelo WhatsApp Web, manualmente.

## Como funciona a seleção (as 5 fases)

O coração do sistema é um **harness multifase** (spec-07): em vez de pedir tudo ao modelo
de uma vez, cada chamada faz uma tarefa pequena e focada. Princípio inviolável: **o LLM só
julga** (escolher trecho, avaliar fidelidade); **o código faz o determinístico** (tempo,
duração, soma de score, parsing). Nunca se pede ao modelo para fazer conta ou copiar
número com precisão.

```
legenda .srt
   │  srtclean + desduplicação (remove a repetição das legendas "rolling": −68% de tokens)
   ▼
transcrição limpa
   │
   ├─ Fase 1 — Mapa            (modelo)  tema central + blocos de ensino, bordas aproximadas
   ├─ Fase 2 — Candidatos      (modelo)  quais blocos viram Short, cada um com uma frase-âncora
   ├─ Fase 3 — Delimitação     (código)  ancora na frase, cresce por frases completas até 30–58s
   ├─ Fase 4 — Avaliação ×2    (modelo)  pontua 5 critérios vs. a Declaração Doutrinária
   └─ Fase 5 — Validação final (código)  rede de segurança determinística
   ▼
candidatos.corrigido.json  →  revisão humana (aprovar/reprovar)  →  render
```

- **Fase 1 — Mapa** *(modelo)*: lê a transcrição e devolve um mapa do sermão (tema central
  + blocos de ensino com início/fim aproximados). É compreensão global; ainda não escolhe
  Short. Recebe a transcrição **desduplicada** (menos tokens, mais estável).
- **Fase 2 — Candidatos** *(modelo)*: dado o mapa + a transcrição, propõe quais blocos
  viram Short, cada um com uma **frase-âncora** — sem timestamp (timestamp é da Fase 3).
- **Fase 3 — Delimitação** *(100% código, sem LLM)*: a partir da frase-âncora, ancora o
  trecho e cresce por **frases completas** até a duração cair em **30–58 s**, com start/end
  sempre em limite de frase. O que não formar 30 s coerentes é descartado. Aqui os tempos
  saem sobre a transcrição bruta (que tem o tempo por palavra).
- **Fase 4 — Avaliação** *(modelo, em duplicata)*: pontua cada trecho em 5 critérios —
  **fidelidade contextual** (teto 30), **valor pastoral** (30), **completude** (20),
  **força de abertura** (10), **formato** (10) — contra a Declaração Doutrinária anexada
  ao prompt. Roda **2×** por robustez; fidelidade baixa ou avaliações divergentes marcam
  `requer_revisao_reforcada` (⚠️) — nunca descartam (spec-11).
- **Fase 5 — Validação final** *(100% código)*: rede de segurança determinística — hook
  alinhado ao start, duração na faixa, `score = soma dos critérios`, descarte de hook
  inventado. Valida sobre a **mesma** base desduplicada da Fase 3.

O `context_fidelity` (0–30) da Fase 4 é o **indicador teológico** do sistema: ele levanta
suspeita de distorção doutrinária da mensagem, mas não certifica — quem certifica é o
pastor. Rode `go run ./cmd/harness -transc ... -ate 5` para ver a saída de cada fase.

## Melhorias implementadas

Resumo do que já está construído além do núcleo de seleção, agrupado por tema. A regra de
ouro se mantém: **nenhuma palavra do pregador é alterada** e a **certificação teológica é
humana** (o pastor aprova na interface). As melhorias tratam de velocidade, estabilidade e
fidelidade do recorte — não de julgar doutrina.

### Interface web do operador (spec-05, Partes 1–2)

- `cmd/servidor` — servidor local (padrão `:7799`, sem auth, página única via embed com
  HTMX). O operador cola link + início/fim e acompanha na tela; não usa terminal.
- **Fluxo invertido / fase leve**: baixa **só a legenda** (sem o vídeo pesado) e roda a
  seleção. O vídeo só é baixado depois da aprovação (economiza banda/tempo).
- **Player YouTube por trecho** (IFrame API): toca do start ao end e para sozinho;
  ▶/⏸ e "assistir de novo". Resiliente a re-render do HTMX; usa `youtube-nocookie`.
- **Aprovar/Reprovar** por trecho + "Confirmar aprovados". Trechos com fidelidade
  duvidosa aparecem com ⚠️ e nunca são escondidos.

```bash
go run ./cmd/servidor                 # sobe em http://localhost:7799
HARNESS_TEMP=0 go run ./cmd/servidor  # seleção reprodutível (ver abaixo)
```

### Estabilidade do modelo (harness)

- **Descasca JSON embrulhado**: respostas em bloco de código markdown (cerca `json`) ou
  cercadas de texto são limpas antes do parse (genérico; JSON puro passa intacto).
- **Truncamento tratado**: `finish_reason=length` vira erro claro e retryável ("resposta
  truncada"), em vez do críptico "unexpected end of JSON input".

### Desempenho

- **Desduplicação na seleção**: a legenda "rolling" do YouTube inflava a transcrição
  ~2–3×. As Fases 1 e 2 passam a receber a versão desduplicada (**−68% de tokens**:
  ~25k → ~8k). Isso eliminou o loop/travamento de modelos quantizados e acelerou tudo.
- **Config do modelo local** (RTX 4000 Ada, 20 GB): ver `docs/otimizacao-modelo-local.md`.
  Recomendado `~/start-gemma-otim.sh` (contexto 24k, `--parallel 1`): **16.2 GB VRAM**,
  funil completo em **25–36 s/sermão**, 0 crashes.
- **Sampling calibrável por ambiente** (sem recompilar): `HARNESS_TEMP`,
  `HARNESS_REPEAT_PENALTY`. `HARNESS_TEMP=0` deixa a seleção quase determinística (os
  melhores trechos saem idênticos entre execuções) sem perder qualidade.

### Fidelidade e auditoria

- **Correção do "hook clipado"**: a Fase 5 validava sobre o texto bruto e deslizava o
  start ~3 s à frente, cortando o começo do hook. Agora valida sobre a mesma base
  desduplicada da Fase 3 — o corte começa no lugar certo.
- `cmd/auditar` — cruza cada candidato validado com a legenda real e acusa: hook clipado,
  hook inventado, corte no meio da fala, duração fora de 30–60 s. Com `-texto`, imprime o
  texto realmente falado (insumo da revisão humana).

```bash
go run ./cmd/auditar -todos           # audita todos os pedidos em trabalho/
go run ./cmd/auditar -id <pedido> -texto
```

### Avaliação de variância

- **Log de rodadas** (`resultados/rodadas.md`): cada seleção concluída vira uma
  "Rodada N" com os candidatos ordenados por score, o título do vídeo e a janela — para
  comparar variância entre sermões/execuções. Configurável com `-log`.

## Como executar as ferramentas

Todas são binários Go (`go run ./cmd/<nome>`). As que falam com o modelo esperam o
`llama-server` no ar (padrão `http://localhost:8080/v1/chat/completions`); as de vídeo
esperam `ffmpeg`; as de download esperam `yt-dlp`.

### Interface web (recomendada para o operador)

```bash
go run ./cmd/servidor                    # sobe em http://localhost:7799
go run ./cmd/servidor -porta 8090 -log resultados/rodadas.md
HARNESS_TEMP=0 go run ./cmd/servidor     # seleção reprodutível
```
Faz tudo pela página: cola link + início/fim, seleciona pela legenda, revisa no player,
aprova. (A fase pesada — baixar vídeo + render dos aprovados — é a Parte 3, pendente.)

### Linha de comando (etapas isoladas / diagnóstico)

```bash
# 1. Limpar uma legenda .srt -> transcrição "[HH:MM:SS] texto"
go run ./cmd/srtclean -in sermao.srt -out sermao.txt -until 00:33:10

# 2. Baixar legenda + vídeo do trecho e gerar a transcrição (precisa de yt-dlp)
go run ./cmd/baixar -url "<link>" -inicio 00:05:30 -fim 00:38:10 -id meu-sermao
#   -force  substitui um id que aponte para outro vídeo

# 3. Selecionar os Shorts (harness completo; precisa do llama-server)
go run ./cmd/selecionar -transc trabalho/meu-sermao/transcricao.txt \
  -out trabalho/meu-sermao/candidatos.corrigido.json

# 3b. Rodar o harness em FASES, para diagnóstico (mostra mapa, candidatos, tempos)
go run ./cmd/harness -transc trabalho/meu-sermao/transcricao.txt -ate 5

# 4. Validar/corrigir candidatos crus do modelo (determinístico, sem LLM)
go run ./cmd/validar -de 1 -ate 5 -corrigir

# 5. Renderizar os Shorts (corte 9:16 + legenda + logo; precisa de ffmpeg)
go run ./cmd/render -id meu-sermao
```

### Auditoria de fidelidade (`cmd/auditar`)

Cruza cada candidato validado com a legenda real e acusa hook clipado, hook inventado,
corte no meio da fala e duração fora de 30–60 s. Não usa LLM — é 100% determinístico.

```bash
go run ./cmd/auditar -todos                     # todos os pedidos em trabalho/
go run ./cmd/auditar -id meu-sermao             # um pedido
go run ./cmd/auditar -id meu-sermao -texto      # inclui o texto falado (revisão humana)
go run ./cmd/auditar -id meu-sermao -criterios  # grade de critérios da Fase 4 (ver abaixo)
go run ./cmd/auditar -todos > resultados/auditoria.md   # salvar o relatório
```
Sai com código ≠ 0 se achar algum problema (útil em verificação automatizada). Com
`-texto`, imprime o texto realmente falado em cada janela — insumo da revisão teológica,
que continua sendo do pastor.

#### Grade de critérios (`-criterios`) — por que o score é aquele

Mostra o que compõe o score de cada trecho (Fase 4). **`score = soma dos 5 critérios`**:

| # | score | fidelidade/30 | pastoral/30 | completude/20 | abertura/10 | formato/10 | revisão |
|---|-------|---------------|-------------|---------------|-------------|------------|---------|
| 1 | 70 | 25 | 20 | 10 | 7 | 8 | — |
| 5 | 88 | 28 | 30 | 15 | 7 | 8 | — |

Como ler:
- **fidelidade** (`context_fidelity`) é o **indicador teológico**: mede se a mensagem do
  trecho é fiel à Declaração Doutrinária. Fidelidade baixa (< 18) ou avaliações em
  duplicata divergentes marcam a coluna **revisão** com ⚠️ — nunca descartam (spec-11).
- **pastoral**, **completude**, **abertura** (força do gancho) e **formato** completam o
  score. Um trecho pode ter score alto e ainda assim abrir mal (abertura baixa) ou ter a
  transcrição ruidosa — por isso a leitura humana continua necessária.
- Limite do indicador: a fidelidade julga o *sentido* que o modelo extrai, não a exatidão
  da legenda. Um erro de ASR que troca uma palavra (ex.: um termo hebraico garbled) pode
  passar com fidelidade alta — quem certifica é o pastor.

### Testes

```bash
go build ./...      # compila tudo
go test ./...       # roda a suíte
```
