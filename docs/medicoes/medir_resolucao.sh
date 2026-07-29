#!/bin/bash
# Compara 1080x1920 com 720x1280 no MESMO trecho: tempo de render, tamanho do arquivo e
# nitidez (variância do laplaciano) da logo e do rosto, na tela de 1080 de largura.
#
# Uso:  docs/medicoes/medir_resolucao.sh <video.mp4> <segundo> [rosto_x rosto_y]
# Ex.:  docs/medicoes/medir_resolucao.sh trabalho/web-20260729-145943-2/video.mp4 3830 270 430
#
# rosto_x/rosto_y é o canto superior esquerdo do recorte de rosto (520x650) em coordenadas do
# Short de 1080x1920. Ajuste ao seu frame: abra o frame que o script deixa em /tmp e confira.
# O recorte da LOGO é fixo — ela está sempre no mesmo lugar.
#
# Reproduz a cadeia de filtros de produção (internal/video/ffmpeg.go): crop 9:16 central +
# scale lanczos + gradiente do rodapé (520/0,60) + logo. Ao emitir 720, os quatro valores em
# pixels de saída são escalados por 2/3 — resolução não é um número só.
set -eu
V=${1:?video de entrada}
T=${2:?segundo do trecho}
FX=${3:-270}
FY=${4:-430}
OUT=$(mktemp -d)
LOGO=assets/ibi_assinatura_shorts.png

render() { # larg alt grad faixa logo_w saida
  ffmpeg -y -v error -ss "$T" -i "$V" -i "$LOGO" -filter_complex \
"[0:v]crop=ih*9/16:ih,scale=$1:$2:flags=lanczos,setsar=1[v0];\
color=c=black:s=$1x$3:d=1,format=rgba,geq=r=0:g=0:b=0:a='0.60*255*pow(Y/H\,2.2)'[grad];\
[v0][grad]overlay=0:H-h[vg];[1:v]scale=$5:-2[logo];[vg][logo]overlay=x=(W-w)/2:y=H-$4/2-h/2+0[vout]" \
    -map "[vout]" -map "0:a?" -t 34 \
    -c:v libx264 -preset medium -crf 18 -c:a aac -b:a 128k -movflags +faststart "$6"
}

echo "== tempo de render (34 s de Short) =="
/usr/bin/time -f "1080x1920: %e s" bash -c "$(declare -f render); V='$V' T='$T' LOGO='$LOGO'; render 1080 1920 520 240 550 $OUT/s1080.mp4"
/usr/bin/time -f " 720x1280: %e s" bash -c "$(declare -f render); V='$V' T='$T' LOGO='$LOGO'; render 720 1280 347 160 367 $OUT/s720.mp4"

echo; echo "== tamanho do arquivo =="
ls -l --block-size=K "$OUT/s1080.mp4" "$OUT/s720.mp4" | awk '{printf "%-12s %s\n", $NF, $5}'

# Um frame de cada. O 720 é ampliado para 1080 porque é o que o player faz numa tela de
# 1080 de largura — é a comparação que o espectador vê.
ffmpeg -y -v error -ss 2 -i "$OUT/s1080.mp4" -frames:v 1 -update 1 "$OUT/f1080.png"
ffmpeg -y -v error -ss 2 -i "$OUT/s720.mp4"  -frames:v 1 -update 1 "$OUT/f720.png"
ffmpeg -y -v error -i "$OUT/f720.png" -vf scale=1080:1920 "$OUT/f720_ampliado.png"
for n in f1080 f720_ampliado; do
  ffmpeg -y -v error -i "$OUT/$n.png" -vf "crop=550:160:265:1720"   "$OUT/logo_$n.png"
  ffmpeg -y -v error -i "$OUT/$n.png" -vf "crop=520:650:$FX:$FY"    "$OUT/rosto_$n.png"
done

echo; echo "== nitidez na tela de 1080 (laplaciano; maior = mais nítido) =="
echo "-- logo --";  go run ./docs/medicoes/nitidez "$OUT/logo_f1080.png"  "$OUT/logo_f720_ampliado.png"
echo "-- rosto --"; go run ./docs/medicoes/nitidez "$OUT/rosto_f1080.png" "$OUT/rosto_f720_ampliado.png"

echo; echo "== contraprova: os dois normalizados para a resolução da FONTE =="
echo "(se derem igual, emitir 720 não perde informação — a perda acima é da ampliação do player)"
ffmpeg -y -v error -i "$OUT/f1080.png" -vf "crop=520:650:$FX:$FY,scale=195:244" "$OUT/n1080.png"
ffmpeg -y -v error -i "$OUT/f720.png"  -vf "crop=347:433:$((FX*2/3)):$((FY*2/3)),scale=195:244" "$OUT/n720.png"
go run ./docs/medicoes/nitidez "$OUT/n1080.png" "$OUT/n720.png"

echo; echo "frames em $OUT"
