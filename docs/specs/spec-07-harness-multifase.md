# Spec 07 — Harness de seleção multifase (redesenho do Selecionar)

## Objetivo

Substituir a seleção de chamada única por um harness em fases, onde cada chamada ao
modelo faz UMA tarefa focada. O objetivo é a **qualidade do resultado**, não a
velocidade — tempo de processamento não é restrição. Isto resolve o problema do
modelo "cansar" numa chamada única (score zerado, duração fora de faixa, trechos
curtos) ao dar-lhe fôlego para cada tarefa.

## Contexto

O `Selecionar` atual (spec-02) faz tudo numa chamada: ler, escolher, marcar tempo,
avaliar 5 critérios de cada trecho. Testes em produção mostraram o modelo caprichando
no 1º candidato e degradando nos seguintes — critérios zerados (score 0), duração de
70 s, trechos de 2 s. Diagnóstico: carga demais por chamada. Ver
`docs/aprendizados-do-spike.md`. Diretriz do dono do projeto: **qualidade acima de
tudo; tempo não importa; pode-se jogar fora código para chegar ao melhor desenho.**

Princípio central (não reabrir): **o modelo só faz o que exige julgamento (entender e
avaliar teologia); todo trabalho determinístico (tempo, contagem, soma, faixa) é do
código.** Cada fase do modelo é uma tarefa pequena.

## Escopo

Dentro:
- Novo pacote de orquestração (ex.: `internal/harness`) que substitui a seleção
  atual, implementando as fases abaixo. O `internal/pipeline.Selecionar` antigo é
  aposentado (removido ou reescrito para chamar o novo harness).
- Cinco fases (detalhe em "Contratos").
- Avaliação em duplicata por candidato, com detecção de divergência.
- Prompts próprios por fase, em `prompts/` (um arquivo por fase).
- Testes com modelo fake (`httptest`) para cada fase e para a orquestração.

Fora (specs próprias):
- Regras de descarte por duração/veto no validador standalone (já existe base na
  validação; o harness usa a mesma lógica — ver "Fase 5").
- Vídeo, servidor, retenção.

## Decisões já tomadas (não reabrir)

- Refazer do zero (não evoluir o `Selecionar` de chamada única).
- Qualidade > tempo. Muitas chamadas ao modelo são aceitáveis.
- Timestamp, contagem, soma e verificação de faixa são SEMPRE do código.
- Parâmetros do modelo por chamada: `temperature 0.2`, `repeat_penalty 1.1`,
  `response_format json_object`, `enable_thinking false`. `max_tokens` dimensionado
  por fase (a fase de mapa pode precisar de mais; a de avaliação, menos).
- Duração de Short: **30–58 s** (limite absoluto 60). Mínimo 30 s.
- Avaliação em duplicata: cada trecho é avaliado 2x; divergência alta →
  `requer_revisao_reforcada: true`.
- Fidelidade teológica é o critério de veto (a Declaração Doutrinária entra na fase
  de avaliação).

## Fases (contrato do harness)

**Fase 1 — Mapa do sermão (1 chamada).**
Entrada: transcrição limpa inteira. Saída: `tema_central`, `estrutura`, e uma lista
de **blocos de ensino** (cada um: assunto + início/fim aproximados). O modelo só
compreende e delimita ideias; não escolhe Short, não avalia. As bordas dos blocos são
aproximadas, mas importam: a fase 3 usa a borda do bloco como o LIMITE dentro do qual
pode crescer um trecho (para não misturar assuntos). No teste real, a fase 1 produziu
um mapa de ótima qualidade — manter esse comportamento.

**Fase 2 — Identificação de candidatos (1 chamada). NÃO emite tempo.**
Entrada: o mapa + a transcrição. Saída, por candidato: qual `bloco` do mapa vira Short
(referência ao bloco da fase 1) e a `frase_ancora` (o hook pretendido — a frase-núcleo
do trecho). **A fase 2 NÃO marca `inicio`/`fim` nem estima duração.** Motivo, com base
no teste real (sermão mg83gcM4ctw): mesmo com a carga aliviada, o modelo aponta regiões
de tamanho errado (viu-se 7 s, 19 s e 65 s). Estimar duração por timestamp é justamente
a tarefa em que o modelo falha; ela é 100% da fase 3. A fase 2 faz só julgamento: "qual
bloco de ensino merece virar Short e qual é a frase que o ancora".

**Fase 3 — Delimitação de tempo (código, sem modelo). Dona de TODO o tempo.**
Para cada candidato, o código:
1. Localiza a `frase_ancora` na transcrição → esse é o ponto de ancoragem (o `start`
   provisório = o `[HH:MM:SS]` onde a âncora começa; vira também o `hook`).
2. **Cresce o trecho** a partir da âncora, respeitando as bordas do `bloco` do mapa
   (nunca invade o assunto do bloco vizinho): estende para frente até o fim de frases
   completas e, se necessário, para trás até o início de frases completas, até a duração
   (`end - start`) cair na faixa **30–58 s**.
3. `start` e `end` sempre caem em limites de frase (início/fim de sentença), nunca no
   meio de uma oração.
4. Se, dentro das bordas do bloco, não for possível formar ao menos 30 s coerentes, o
   candidato é **descartado como inviável** (registrar o motivo).
Nenhum timestamp vem do modelo. A detecção de limite de frase é lógica pura e testável.

**Fase 4 — Avaliação por candidato, em duplicata (2 chamadas por candidato).**
Entrada: o texto do trecho já recortado + as regras de pontuação + a Declaração
Doutrinária. Saída: os 5 critérios e observações. Feita 2x. O código então:
- calcula o `score` = soma dos critérios (média das duas rodadas, ou a menor — ver
  "questão em aberto");
- se as duas rodadas divergem além de um limiar (ex.: fidelidade difere > 8, ou uma
  aprova e outra veta), marca `requer_revisao_reforcada: true`;
- se qualquer rodada dá fidelidade abaixo do veto, o candidato é vetado.

**Fase 5 — Validação final (código).**
Aplica a rede de segurança determinística: duração em 30–60 s; `start` alinhado ao
hook; score coerente (soma); veto por fidelidade; descarta o que violar. Reusa a
lógica de `internal/validacao`. Nenhum candidato não-avaliado (score 0) passa.

## Contratos e interfaces

`Selecionar(ctx, transcricaoPath) ([]Candidato, error)` — mesma assinatura externa de
antes (o resto do sistema não muda), mas internamente roda as 5 fases. Cada fase é uma
função testável isolada, com o modelo atrás de uma interface `ModeloLLM` mockável.
Candidato final tem os campos já conhecidos (start, end, duration_seconds, score,
hook, reason, complete_thought, requer_revisao_reforcada, criteria) — agora sempre
preenchidos e validados.

## Questões em aberto (decidir na execução, registrar a escolha)

- Score final da duplicata: média das duas rodadas ou a menor (mais conservador)?
  Recomendação: a **menor**, porque prioriza fidelidade/cautela.
- Limiar de divergência para `requer_revisao_reforcada`: começar com "fidelidade
  difere > 8 pontos OU vereditos de veto discordam" e ajustar com dados.

## Critérios de aceite

- [ ] `Selecionar` roda as 5 fases; a de chamada única foi removida.
- [ ] Cada fase é função isolada, testada com modelo fake (`httptest`).
- [ ] Fase 2 NÃO emite timestamp: sua saída por candidato é só `bloco` + `frase_ancora`
      (verificado no tipo de dados e nos testes).
- [ ] Fase 3 (tempo) é 100% código: nenhum timestamp vem do modelo; testes provam que
      start/end caem em limites de frase, a duração fica em 30–58 s, e o trecho não
      ultrapassa as bordas do bloco do mapa.
- [ ] Fase 3 descarta candidato inviável (não forma 30 s dentro do bloco) e registra o
      motivo, em vez de emitir um trecho curto.
- [ ] Avaliação em duplicata: teste com duas respostas divergentes marca
      `requer_revisao_reforcada`; com duas concordantes, não marca.
- [ ] Nenhum candidato com score 0 / critérios zerados / duração fora de 30–60 s
      chega ao resultado final.
- [ ] Teste ponta a ponta com modelo fake produz candidatos completos e válidos.
- [ ] `go build ./...` e `go test ./...` verdes.
- [ ] Teste manual real com o sermão `mg83gcM4ctw`: comparar o resultado com o da
      chamada única (durações, scores preenchidos, nenhum trecho de 2 s). Registrar.

## Como validar

```bash
go test ./...
# real, com llama-server no ar:
go run ./cmd/selecionar -transc trabalho/sermao/transcricao.txt \
  -out trabalho/sermao/candidatos.corrigido.json -prompt-dir prompts/
python3 -c 'import json;[print(c["duration_seconds"],"s",c["score"],c["hook"][:40]) for c in json.load(open("trabalho/sermao/candidatos.corrigido.json"))["candidatos"]]'
# esperado: durações em 30-58s, scores preenchidos (nenhum 0), sem trechos curtos.
```

## Fora de escopo / próximos passos

Refinamento visual (enquadramento 9:16, desduplicação da legenda rolling, posição de
legenda e logo) — specs próprias, já mapeadas com o dono do projeto.