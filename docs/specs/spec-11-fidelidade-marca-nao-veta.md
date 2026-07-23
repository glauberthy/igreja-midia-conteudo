# Spec 11 — Veto de fidelidade vira marcação para revisão humana (nunca descarta)

## Objetivo

Mudar o comportamento do critério de fidelidade: hoje, fidelidade baixa **veta e
descarta** o trecho (ele some, o humano nunca vê). Passa a ser: fidelidade baixa
**marca o trecho para revisão humana** e o mantém no resultado. O modelo perde o poder
de descartar por fidelidade; ganha o papel de **levantar suspeita**. A decisão final de
fidelidade é sempre humana.

## Motivação (caso real que expôs o problema)

No sermão `culto-noite-19-07-26` (João 17), o funil (`harness -ate 5`) mostrou que a
Fase 4 **vetou por fidelidade dois trechos que são citação direta da Bíblia** — a oração
sacerdotal de Jesus em João 17 ("A minha oração não é apenas por eles. Peço também por
aqueles que crerão em mim..."). Ou seja, o veto removeu os trechos MAIS fiéis (Escritura
pura), enquanto trechos confusos de score baixo (47) passaram. Causa provável: a legenda
automática transcreveu a citação com pontuação errada ("para que todos sejam um Pai"), e
o modelo pequeno interpretou como distorção; ou simplesmente errou o julgamento de
fidelidade (tarefa difícil para modelo quantizado pequeno).

Conclusão de projeto (decisão do dono): **o modelo não é confiável o suficiente para ter
poder de VETO sobre fidelidade, mas é útil para LEVANTAR SUSPEITA.** Em decisões de
fidelidade teológica, o modelo sugere; o humano decide. Um veto destrutivo causa perda
silenciosa de bons trechos (como Escritura), o que é inaceitável num pipeline cuja
prioridade nº 1 é fidelidade.

## Decisão (não reabrir)

- O modelo **NUNCA descarta** um trecho por fidelidade. Não há mais "veto" destrutivo por
  fidelidade.
- Quando a fidelidade avaliada é baixa (abaixo do limiar que antes vetava), o trecho é
  **mantido** e recebe uma marcação de revisão: `requer_revisao_reforcada = true` com um
  motivo claro (ex.: `motivo_revisao: "possível problema de fidelidade — revisar"`).
- O trecho marcado chega ao operador COM o alerta visível, para ele julgar e aprovar ou
  descartar manualmente.
- Isto reaproveita o mecanismo `requer_revisao_reforcada` já existente (criado para
  divergência na avaliação em duplicata) — estende o mesmo conceito ao caso de fidelidade.

## Escopo

Dentro:
- Fase 4 (`internal/harness/fase4.go` / `CombinarAvaliacoes`): fidelidade abaixo do limiar
  NÃO veta; marca `requer_revisao_reforcada = true` e registra o motivo. O score continua
  sendo calculado normalmente (a fidelidade baixa naturalmente reduz o score, o que é ok —
  isso ordena o trecho para baixo, mas não o elimina).
- Fase 5 (`internal/harness/fase5.go`): remover o descarte por veto de fidelidade. A Fase 5
  continua descartando pelos critérios OBJETIVOS e mensuráveis (duração fora de 30–60 s,
  hook inexistente/desalinhado, score zerado por ausência de avaliação) — esses NÃO são
  julgamento subjetivo, são fatos, e continuam válidos. Só o descarte por fidelidade
  (julgamento subjetivo do modelo) é que sai.
- Garantir que o motivo da marcação seja legível no resultado (para o operador e para a
  futura interface web).

Fora:
- Não muda a forma de calcular os critérios nem o score.
- Não mexe nos descartes objetivos (duração, hook, score zerado) — esses continuam.

## Distinção importante (o que continua descartando vs o que vira marcação)

- **Continua DESCARTANDO (fatos objetivos):** duração fora de 30–60 s; hook não encontrado
  na transcrição; ausência de avaliação (score/critérios zerados por falha de formato).
  São mensuráveis, não são opinião.
- **Vira MARCAÇÃO (julgamento subjetivo):** fidelidade teológica baixa. É opinião do
  modelo, e o modelo não é confiável nela — então marca, não descarta.

## Critérios de aceite

- [ ] Nenhum trecho é descartado por fidelidade baixa; em vez disso é mantido com
      `requer_revisao_reforcada = true` e um motivo legível.
- [ ] Os descartes objetivos (duração, hook, score zerado) continuam funcionando.
- [ ] No sermão `culto-noite-19-07-26`, os dois trechos de João 17 antes vetados agora
      aparecem no resultado final, marcados para revisão (não somem).
- [ ] O motivo da marcação é visível no JSON final.
- [ ] Teste: um trecho com fidelidade abaixo do limiar antigo NÃO é descartado, e sai
      marcado; um trecho com duração fora de faixa CONTINUA sendo descartado.
- [ ] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./...
go run ./cmd/harness -transc "trabalho/culto-noite-19-07-26/transcricao.txt" -ate 5 \
  -out-final "trabalho/culto-noite-19-07-26/finais.json"
# esperado: os trechos de João 17 aparecem, marcados requer_revisao_reforcada=true,
# em vez de sumirem por veto.
```

## Nota de princípio

Esta spec cristaliza um princípio do projeto: **decisões objetivas e mensuráveis podem
ser automáticas (código); decisões subjetivas de julgamento teológico ficam com o humano,
assistido por marcações do modelo.** O modelo aponta; o humano decide. Vale rever, no
futuro, se outros "vetos" subjetivos no sistema deveriam seguir o mesmo caminho.
