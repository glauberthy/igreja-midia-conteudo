#!/usr/bin/env bash
#
# avaliar_sermoes.sh
# Percorre transcricao_1.txt .. transcricao_N.txt, envia cada uma ao modelo
# local (llama.cpp) com o prompt de seleção, e salva o JSON de candidatos
# em resultados/candidatos_1.json .. candidatos_N.json.
#
# Uso (rodar a partir da raiz do projeto):
#   ./scripts/avaliar_sermoes.sh          # processa transcricao_1..5
#   ./scripts/avaliar_sermoes.sh 1 3      # processa transcricao_1..3
#
# Requer: curl, jq, e o llama-server rodando em $ENDPOINT.

set -euo pipefail

# ---- Configuração (ajuste se precisar) ----
ENDPOINT="http://localhost:8080/v1/chat/completions"
PROMPT="prompts/selecao_shorts_v0_1.md"
PREFIXO="transcricao_"
SAIDA="resultados"
INICIO="${1:-1}"
FIM="${2:-5}"

# ---- Verificações ----
command -v jq   >/dev/null || { echo "erro: jq não encontrado"; exit 1; }
command -v curl >/dev/null || { echo "erro: curl não encontrado"; exit 1; }
[ -f "$PROMPT" ] || { echo "erro: prompt '$PROMPT' não encontrado"; exit 1; }

if ! curl -s -o /dev/null --max-time 5 "${ENDPOINT%/v1/*}/health"; then
  echo "aviso: não consegui acessar o servidor em $ENDPOINT — verifique se o llama-server está de pé."
fi

mkdir -p "$SAIDA"
echo "Processando transcrições $INICIO a $FIM..."
echo ""

falhas=0
for i in $(seq "$INICIO" "$FIM"); do
  arquivo="${PREFIXO}${i}.txt"
  destino="${SAIDA}/candidatos_${i}.json"
  bruto="${SAIDA}/bruto_${i}.json"   # resposta completa, para diagnóstico

  if [ ! -f "$arquivo" ]; then
    echo "  [$i] pulado: '$arquivo' não existe"
    continue
  fi

  printf "  [%s] %s ... " "$i" "$arquivo"

  # Monta o payload com jq (escapa tudo automaticamente)
  jq -n \
    --rawfile sys "$PROMPT" \
    --rawfile transcricao "$arquivo" \
    '{
       temperature: 0.2,
       max_tokens: 3000,
       repeat_penalty: 1.1,
       response_format: { type: "json_object" },
       chat_template_kwargs: { enable_thinking: false },
       messages: [
         { role: "system", content: $sys },
         { role: "user",   content: $transcricao }
       ]
     }' > "${SAIDA}/payload_${i}.json"

  # Chama o modelo, guardando a resposta crua
  if ! curl -s "$ENDPOINT" \
        -H "Content-Type: application/json" \
        -d @"${SAIDA}/payload_${i}.json" > "$bruto"; then
    echo "FALHA (curl)"
    falhas=$((falhas + 1))
    continue
  fi

  # Extrai só o content (o JSON de candidatos) e valida que é JSON de verdade
  conteudo=$(jq -r '.choices[0].message.content // empty' "$bruto")
  finish=$(jq -r '.choices[0].finish_reason // "?"' "$bruto")

  if [ -z "$conteudo" ]; then
    echo "VAZIO (finish_reason=$finish) — veja $bruto"
    falhas=$((falhas + 1))
    continue
  fi

  if echo "$conteudo" | jq empty 2>/dev/null; then
    echo "$conteudo" | jq '.' > "$destino"
    n=$(echo "$conteudo" | jq '.candidatos | length')
    echo "OK (${n} candidatos) -> $destino"
  else
    echo "JSON INVÁLIDO (finish_reason=$finish) — salvo cru em $bruto"
    echo "$conteudo" > "${SAIDA}/invalido_${i}.txt"
    falhas=$((falhas + 1))
  fi
done

echo ""
if [ "$falhas" -eq 0 ]; then
  echo "Concluído. Resultados em ./$SAIDA/"
else
  echo "Concluído com $falhas falha(s). Verifique os arquivos bruto_*.json em ./$SAIDA/"
fi
