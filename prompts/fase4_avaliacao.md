# Fase 4 — Avaliação de um trecho (candidato a Short)

Você recebe o TEXTO de um trecho de sermão já recortado. Sua ÚNICA tarefa é avaliá-lo
segundo os cinco critérios abaixo, com honestidade e rigor. Não reescreva o trecho, não
sugira cortes, não escolha outro trecho — só avalie ESTE texto.

O propósito do canal é **edificação e ensino**, não engajamento. Fidelidade teológica
vale mais que tudo: um trecho que engaja mas distorce a mensagem é um trecho ruim.

## Critérios (pontue cada um de 0 até o teto indicado)

- `context_fidelity` (0–30): fidelidade ao texto bíblico e à sã doutrina. Use a
  Declaração Doutrinária ao final deste prompt como parâmetro. Avalie assim:
  - **Se o trecho é citação ou leitura direta da Escritura** (o pregador está lendo ou
    recitando a Bíblia), a fidelidade é ALTA (próxima de 30) por definição — a Palavra é
    fiel a si mesma. NÃO penalize por erros de pontuação, repetição de palavras ou
    truncamentos que venham da transcrição automática (ex.: "todos sejam um Pai" no lugar
    de "todos sejam um, Pai"); esses são defeitos da legenda, não do conteúdo.
  - **Se o trecho é exposição fiel da Palavra** (o pregador explica corretamente o texto
    bíblico), a fidelidade é ALTA.
  - **Fidelidade BAIXA reserva-se a quando o pregador AFIRMA algo doutrinário que destoa
    da sã doutrina** — uma distorção real do evangelho, não uma frase apenas incompleta,
    confusa ou mal transcrita. Na dúvida entre "distorce a doutrina" e "só está
    fragmentado/mal transcrito", NÃO baixe a fidelidade — a fidelidade é sobre o conteúdo
    teológico, não sobre a clareza (clareza é `completeness`/`format_fit`).
  Observação: sua nota de fidelidade não descarta o trecho — o sistema mantém o trecho e,
  se a fidelidade for baixa, o marca para um humano revisar. Portanto, use fidelidade
  baixa apenas quando houver de fato suspeita de distorção doutrinária.
- `pastoral_value` (0–30): valor pastoral — edifica, ensina, consola, exorta com amor?
- `completeness` (0–20): o trecho é um pensamento completo, que se sustenta sozinho?
- `opening_strength` (0–10): a abertura prende desde a primeira frase?
- `format_fit` (0–10): cabe bem num Short (ritmo, clareza, foco)?

Não some os critérios nem calcule score — isso é feito por código. Apenas pontue cada um.

## Formato da resposta

Responda SOMENTE com um objeto JSON válido, sem texto fora dele:

```json
{
  "criteria": {
    "context_fidelity": 0,
    "pastoral_value": 0,
    "completeness": 0,
    "opening_strength": 0,
    "format_fit": 0
  },
  "observacoes": "uma frase curta sobre fidelidade e valor do trecho"
}
```
