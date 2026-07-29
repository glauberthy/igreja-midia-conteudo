#!/bin/bash
# Mede quanto de nitidez o Short perde em relação ao vídeo-fonte, na MESMA cena: recorte do
# rosto nos dois, normalizado para a resolução real da fonte, variância do laplaciano.
#
# Uso:  docs/medicoes/medir_nitidez_rosto.sh <short.mp4> <seg_short> <fonte.mp4> <seg_fonte> [rosto_x rosto_y]
# Ex.:  docs/medicoes/medir_nitidez_rosto.sh finalizados/ID/short_01.mp4 2 trabalho/ID/video.mp4 3830 270 430
#
# rosto_x/rosto_y é o canto superior esquerdo do recorte (520x650) em coordenadas do Short de
# 1080x1920; o recorte equivalente na fonte é calculado a partir do crop 9:16 central. Confira
# a olho que os dois recortes pegaram a mesma região: o script imprime a `luma` de cada um, e
# ela bate em ~0,1 quando é a mesma coisa. Se a luma divergir, o instante está errado e o
# número de nitidez não vale nada.
#
# Normaliza para a resolução da FONTE (195x244) de propósito: ali nenhum dos dois ganha
# interpolação minha. Comparar em tamanho maior mede o meu upscale, não a nitidez.
set -eu
SHORT=${1:?short.mp4}
TS=${2:?segundo no short}
FONTE=${3:?video fonte}
TF=${4:?segundo na fonte}
FX=${5:-270}
FY=${6:-430}
OUT=$(mktemp -d)

# Geometria do crop 9:16 central da fonte (o mesmo que internal/video/ffmpeg.go faz).
LARG=$(ffprobe -v error -select_streams v -show_entries stream=width  -of csv=p=0 "$FONTE")
ALT=$(ffprobe  -v error -select_streams v -show_entries stream=height -of csv=p=0 "$FONTE")
CROPW=$((ALT * 9 / 16))
OFFX=$(((LARG - CROPW) / 2))
ESC=$(echo "1080 / $CROPW" | bc -l)           # fator de ampliação do render
sx=$(printf '%.0f' "$(echo "$OFFX + $FX / $ESC" | bc -l)")
sy=$(printf '%.0f' "$(echo "$FY / $ESC" | bc -l)")
sw=$(printf '%.0f' "$(echo "520 / $ESC" | bc -l)")
sh=$(printf '%.0f' "$(echo "650 / $ESC" | bc -l)")

echo "fonte ${LARG}x${ALT} -> crop 9:16 de ${CROPW}px (offset x=$OFFX), ampliação ${ESC}x"
echo "recorte no Short: 520x650 em ($FX,$FY)   ->   na fonte: ${sw}x${sh} em ($sx,$sy)"
echo "normalizando os dois para ${sw}x${sh} (resolução real da fonte)"
echo

ffmpeg -y -v error -ss "$TS" -i "$SHORT" -frames:v 1 -update 1 "$OUT/short.png"
ffmpeg -y -v error -ss "$TF" -i "$FONTE" -frames:v 1 -update 1 "$OUT/fonte.png"
ffmpeg -y -v error -i "$OUT/short.png" -vf "crop=520:650:$FX:$FY,scale=$sw:$sh" "$OUT/rosto_short.png"
ffmpeg -y -v error -i "$OUT/fonte.png" -vf "crop=$sw:$sh:$sx:$sy"               "$OUT/rosto_fonte.png"

go run ./docs/medicoes/nitidez "$OUT/rosto_fonte.png" "$OUT/rosto_short.png"
echo
echo "a coluna luma dos dois tem de bater (~0,1). recortes em $OUT"
