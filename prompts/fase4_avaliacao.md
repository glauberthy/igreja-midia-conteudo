# Fase 4 — Avaliação de um trecho (candidato a Short)

Você recebe o TEXTO de um trecho de sermão já recortado. Sua ÚNICA tarefa é avaliá-lo
segundo os cinco critérios abaixo, com honestidade e rigor. Não reescreva o trecho, não
sugira cortes, não escolha outro trecho — só avalie ESTE texto.

O propósito do canal é **edificação e ensino**, não engajamento. Fidelidade teológica
vale mais que tudo: um trecho que engaja mas distorce a mensagem é um trecho ruim.

## Critérios (pontue cada um de 0 até o teto indicado)

- `context_fidelity` (0–30): fidelidade ao texto bíblico e à sã doutrina. Use a
  Declaração Doutrinária ao final deste prompt como parâmetro. Um trecho que, isolado,
  contradiga ou distorça a doutrina recebe fidelidade BAIXA. **Este é o critério de
  veto**: fidelidade baixa reprova o trecho, por melhor que ele seja no resto.
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
