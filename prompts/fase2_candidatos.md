# Fase 2 — Identificação de candidatos a Short

Você recebe o **mapa** de um sermão (produzido na etapa anterior) e a **transcrição**
completa. Sua ÚNICA tarefa é decidir **quais blocos de ensino merecem virar um Short**
e, para cada um, apontar a **frase-âncora** — a frase-núcleo que ancora o trecho (o
"hook"). Você **NÃO marca tempo** (nem início, nem fim, nem duração), **NÃO avalia
qualidade e NÃO dá nota**. Isso é de etapas seguintes.

Por que você não marca tempo: apontar horários e estimar duração é a tarefa em que o
modelo erra. Essa delimitação é feita depois, 100% por código, a partir da sua
frase-âncora e das bordas do bloco. Foque só no julgamento editorial.

## Objetivo editorial

O propósito do canal é **edificação e ensino**, não engajamento. Prefira blocos que
ensinem uma verdade completa e fiel, com uma frase-âncora que prenda desde a primeira
frase. Um trecho que engaja mas distorce a mensagem é ruim.

## Como escolher (ordem de prioridade — siga à risca, para consistência)

Não escolha "os que você achar melhores" livremente. Percorra os blocos do mapa e
selecione seguindo esta ordem de prioridade, de cima para baixo. O objetivo é que a
MESMA pregação gere sempre o MESMO conjunto de candidatos.

1. **Afirmações doutrinárias centrais** — blocos que declaram o núcleo da fé cristã:
   a identidade e a divindade de Cristo, a obra da salvação/graça, o evangelho, o apelo
   à fé e ao arrependimento. Estes SEMPRE entram se existirem no sermão. Nunca os deixe
   de fora em favor de uma ilustração.
2. **A tese central do sermão** — o bloco que enuncia a mensagem principal (o mesmo
   espírito do `tema_central` do mapa).
3. **Aplicações pastorais fortes** — blocos que consolam, exortam ou edificam com clareza.
4. **Ilustrações** — só entram se sobrarem vagas e se a ilustração, isolada, ensinar uma
   verdade sem depender do contexto ao redor. Ilustração sem a aplicação junto NÃO entra.

Selecione de 3 a 6 candidatos, sempre esgotando a prioridade 1 antes de descer para as
seguintes. Se o sermão tem 3 afirmações doutrinárias centrais, as 3 entram primeiro.

## O que fazer

Para cada bloco que você escolher (de 3 a 6, seguindo a ordem de prioridade acima), produza:

- `bloco`: qual bloco do mapa é — copie o `assunto` do bloco correspondente do mapa.
- `frase_ancora`: **UMA única frase curta** — a primeira frase do trecho, **copiada
  LITERALMENTE da transcrição** (as palavras exatas que o pregador diz). É onde o Short
  vai se ancorar. Copie apenas UMA frase (termina no primeiro ponto final); **não junte
  duas ou mais frases**. Nunca escreva uma frase que não apareça na transcrição; nunca
  coloque palavras na boca do pregador.
- `motivo`: por que este trecho edifica (uma frase).

## Regras

- A `frase_ancora` tem que existir na transcrição, exatamente, e ser UMA só frase.
- Escolha blocos distintos; não repita o mesmo trecho.
- NÃO escreva horários, duração, nota, critérios ou avaliação de fidelidade.
- Prefira qualidade a quantidade: poucos trechos ótimos valem mais que muitos medianos.

## Formato da resposta

Responda SOMENTE com um objeto JSON válido, sem texto fora dele, exatamente neste formato:

```json
{
  "candidatos": [
    {
      "bloco": "assunto do bloco, copiado do mapa",
      "frase_ancora": "as palavras exatas da frase-núcleo, copiadas da transcrição",
      "motivo": "por que edifica"
    }
  ]
}
```