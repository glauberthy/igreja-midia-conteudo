# Medições da rodada "sem legenda queimada" (2026-07-29)

Rodada em que a queima da legenda foi suspensa (ver spec-12) e o foco passou a ser
**qualidade de imagem**. Tudo aqui é medido, não estimado. Quatro perguntas:

1. o gradiente do rodapé pode encolher? quanto de imagem isso devolve?
2. vale emitir 720×1280 em vez de 1080×1920?
3. o `yt-dlp` está pegando a variante 720p mais fraca?
4. quanto de nitidez o Short perde em relação à fonte?

## Ferramenta

`docs/medicoes/nitidez/` — variância do laplaciano em Go puro (a máquina não tem
OpenCV/numpy, então a medição do dono em `cv2` não roda aqui). Mesma luma
(0,299/0,587/0,114) e mesmo kernel 3×3 do `cv2.Laplacian`.

```bash
go run ./docs/medicoes/nitidez recorte_a.png recorte_b.png
```

O recorte e a normalização de tamanho ficam **fora** da ferramenta, num `ffmpeg` visível na
linha de comando. Motivo: a variância do laplaciano cresce com a resolução, então comparar
duas imagens só é honesto no **mesmo tamanho** — deixar isso explícito evita a comparação
errada por descuido.

> **Cuidado que já mordeu aqui:** conferir se dois recortes são a mesma região pela `luma`
> média. Ela bate em 0,1 quando é o mesmo frame; quando não bate (58,3 contra 64,3), o
> "mesmo instante" era outro frame, e a comparação de nitidez não valia nada.

---

## 1. Gradiente do rodapé: 1500/0,72 → 420/0,90

O gradiente servia a dois fins: contraste da legenda e legibilidade da logo branca. Sem
legenda sobrou a logo, que ocupa os **240 px de baixo** — mas o gradiente cobria **1500 px,
78% da altura do Short**. Era isso que dava a sensação de "apertado".

Nove variantes renderizadas pelo caminho real (`cmd/render` com
`-rodape-altura/-rodape-escuro/-faixa-logo`), no mesmo frame, com o pregador de **camisa
branca** — o pior caso para a logo branca.

Duas medidas, porque o trade-off tem dois lados:

- **torso** (recorte 1080×700 em y=1000): luma média. Maior = mais imagem do pregador
  preservada. Teto sem gradiente nenhum = **122,33**.
- **sob o texto da logo** (recorte 330×158 na altura do texto, renderizado *sem* a logo para
  medir só o fundo): luma média. Menor = mais contraste para o texto branco. Sem gradiente
  = **166,06**, e aí o texto branco quase desaparece na camisa.

| variante | altura/alpha/faixa | torso ↑ | sob o texto ↓ |
|---|---|---|---|
| a (antes) | 1500 / 0,72 / 240 | 87,96 | 68,08 |
| b | 700 / 0,65 / 240 | 114,00 | 97,65 |
| c | 520 / 0,60 / 240 | 119,03 | 113,29 |
| **i (escolhida)** | **420 / 0,90 / 240** | **120,16** | **99,64** |
| j | 480 / 1,00 / 240 | 118,11 | 83,04 |
| d | 420 / 0,55 / 200 | 121,04 | 106,16 |
| e | 340 / 0,72 / 180 | 121,82 | 93,51 |
| g | 500 / 0,80 / 300 | 118,44 | 123,02 |
| f | sem gradiente | 122,33 | 166,06 |

**420/0,90** recupera 94% da imagem que o gradiente antigo escurecia (87,96 → 120,16, de um
teto de 122,33) e ainda escurece o fundo da logo de 166 para 100.

Duas observações contra-intuitivas que a medição resolveu:

- **a opacidade máxima SUBIU** (0,72 → 0,90) enquanto o gradiente clareia a imagem. Não é
  contradição: com rampa de 1500 px, 0,90 apagaria o pregador; com 420 px o topo do
  gradiente fica abaixo do peito, e a opacidade só age onde a logo está.
- **descer a logo** (faixa 240 → 180, variantes d/e) melhora os dois números, porque põe o
  texto na parte escura da rampa. Não foi adotado: com faixa 180 a logo fica a 11 px da
  borda inferior, dentro da área onde o player do Shorts desenha barra de progresso e
  descrição. A faixa fica em 240 (41 px de margem), a posição que o operador já escolheu.

Frames para conferir a olho: `docs/mockups/rodape-sem-legenda/`
(`a_antes_1500_0.72.png`, `c_520_0.60.png`, `i_420_0.90_ESCOLHIDO.png`, `j_480_1.00.png`,
`f_sem_gradiente.png`, e o recorte ampliado `zoom_area_da_logo_a_c_i_j.png`).

---

## 2. Resolução de saída: manter 1080×1920

O argumento para manter 1080 era "legenda e logo são rasterizadas nativamente". Sem legenda
sobra a logo — então a pergunta foi remedida do zero. Mesma cena, mesma cadeia de filtros de
produção, `medium/crf18`, duas rodadas cada:

| | tempo/short | arquivo (34 s) |
|---|---|---|
| 1080×1920 | 4,12 s | 17,9 MB |
| 720×1280 | 2,22 s | 9,5 MB |

720 é **1,85× mais rápido** e **47% menor** — confirma a estimativa (1,8× / 48%).

Nitidez, com os dois recortes normalizados para o **mesmo tamanho**:

| recorte | 1080 nativo | 720 ampliado p/ 1080 |
|---|---|---|
| logo (550×160) | **1092,57** | 388,55 (**−64%**) |
| rosto (520×650) | **9,83** | 6,15 (**−37%**) |

E a contraprova que mostra *de onde* vem a perda — os dois normalizados para a resolução da
**fonte** (195×244):

| | laplaciano |
|---|---|
| fonte 720p (o que baixamos) | 125,40 |
| render 1080 | 97,68 |
| render 720 | 98,23 |

**Nada da fonte se perde emitindo 720** (98,23 ≈ 97,68). A perda de 37% no rosto aparece só
na tela: emitindo 720 nós entregamos a ampliação ao **player** (bicúbico) em vez de fazê-la
no render com **lanczos**, e ainda passamos a reamostrar duas vezes (405→720→1080 em vez de
405→1080).

Ou seja, o argumento "o vídeo não perde nada porque já é upscale de 405 px" é meia verdade:
não perde *informação*, mas perde *nitidez na tela*. Somado aos 64% da logo, o ganho de
1,9 s por Short e 8 MB por arquivo não paga — num ciclo em que o download leva minutos.

**Recomendação: manter 1080×1920.** Comparação visual da logo:
`docs/medicoes/logo_1080_nativo_vs_720_ampliado.png` (topo nativo, base ampliada).

Nota para quem for mexer nisso um dia: a resolução **não** é um número só. Tamanho de fonte,
largura da logo, altura do gradiente e faixa da logo estão todos em pixels de saída; emitir
720 exige escalar os quatro (foi o que a medição fez à mão: logo 550→367, gradiente
420→280, faixa 240→160).

---

## 3. Variantes 720p no `yt-dlp`: nenhum ganho recuperável

`yt-dlp -F` num culto (`fZGyLBofmmo`) mostra **duas** variantes 720p:

```
136 mp4 1280x720 | 816.34MiB 1026k https | avc1.64001f  video only
95  mp4 1280x720 | ~  1.90GiB 2447k m3u8  | avc1.64001F  mp4a.40.2
```

Parecia o achado: 2447k contra 1026k, e o seletor pega o 136. **Mas a diferença não existe
nos bits.** Baixei 35 s de cada e medi:

| formato | bitrate de vídeo medido | laplaciano (rosto, 195×244) |
|---|---|---|
| 136 (DASH, "1026k") | 835,8 kbps | 125,40 |
| 95 (HLS, "2447k") | 837,7 kbps | 129,50 |

O `2447k` é o `BANDWIDTH` declarado no manifesto HLS (pico), não a taxa real. Os dois são o
mesmo encode. (Confirmação de brinde: o recorte do nosso `video.mp4` de 903 MB dá 125,40
**exatamente** — é o formato 136 mesmo.)

Dois detalhes operacionais descobertos no caminho:

- o formato 95 só aparece com `--extractor-args "youtube:player_client=web_safari"`;
- com `--download-sections`, o `yt-dlp` nem busca o manifesto HLS (as variantes m3u8
  desaparecem da lista).

**O caso do codec, que é o real.** Em cultos que o YouTube já transcodificou por completo há
três variantes 720p, e aí o seletor troca de codec:

```
IxmiQGL9CMQ:  136 h264 506k | 247 vp9 387k | 398 av01 269k
FormatoPadrao seleciona -> 398 (AV1, 269k)  =  1,9x menos bits que o h264
```

Média do laplaciano em **16 frames** do mesmo recorte de rosto (um frame só não serve: a
dispersão vai de 16 a 61 por causa do movimento):

| formato | bitrate | laplaciano médio (n=16) |
|---|---|---|
| 136 h264 | 527 kbps | 39,26 |
| 247 vp9 | 387 kbps | 35,83 |
| 398 av01 | 283 kbps | 37,22 |

A diferença é ruído (desvio por frame ≈ 8; erro do médio ≈ 2). **AV1 com metade dos bits
empata com h264** — o seletor não está pegando a variante mais fraca, está pegando a mais
eficiente, e de graça baixa 1,9× menos.

**Recomendação: não mexer em `FormatoPadrao`.**

E a variação de 124 MB a 993 MB no mesmo 720p fica explicada: é o **culto**, não a escolha
de variante. `fZGyLBofmmo` tem 720p a 1026k; `IxmiQGL9CMQ`, a 506k; `D5gVL387nCg`, 1071k.
Quando a fonte é fraca, é fraca na origem — não há o que recuperar. Nenhum dos quatro cultos
testados oferece 1080p, então o teto de 1080 do seletor continua correto e ocioso.

---

## 3b. Ciclo completo, ponta a ponta, com tudo isso

Rodado pelo **caminho do operador** (os mesmos endpoints HTTP que a página usa: `POST
/pedidos` → polling → `POST /pedidos/{id}/aprovar`), no culto
`xZNTJcehAV0` (Pr. Antonio Andrade, 19/07/26, matinal), janela 00:49:15–01:24:30.

Tempo por etapa, do próprio `resultados/tempos.csv` (o servidor mede, não eu):

| etapa | tempo |
|---|---|
| baixar legenda | 3,1 s |
| selecionar (Fases 1–5, Gemma local) | 27,0 s |
| validar | 0,0 s |
| baixar vídeo (819,8 MB, inteiro) | 34,9 s |
| renderizar 4 Shorts | 25,2 s (**6,3 s/short**) |
| **total de máquina** | **90,3 s** |
| espera humana (revisão) | 103,4 s — fora do total, é tempo de pessoa |

Sermão de 35 min, ~23 mil tokens de transcrição, 4 candidatos → 4 aprovados, **0 retries**.

Duração real dos Shorts, medida no arquivo, contra o que a Fase 3 delimitou:

| arquivo | score | pedido | medido | dimensões |
|---|---|---|---|---|
| short_01.mp4 | 90 | 37 s | **37,0 s** | 1080×1920 |
| short_02.mp4 | 86 | 48 s | **48,0 s** | 1080×1920 |
| short_03.mp4 | 81 | 46 s | **46,0 s** | 1080×1920 |
| short_04.mp4 | 76 | 30 s | **30,0 s** | 1080×1920 |

Exato, porque `-margem-fim` é 0 no caminho do servidor (spec-10). Todos dentro da faixa de
30–58 s.

O render subiu de 3,3 s para 6,3 s por Short em relação ao pedido anterior do CSV — não é
regressão do gradiente: o pedido anterior renderizou **1** Short de 34 s e este renderizou
**4**, com duração média maior (40 s contra 34 s).

Conferência do resultado: os quatro Shorts saem **sem legenda**, **com a logo**, e a pasta de
trabalho fica **sem nenhum** `short_NN.subNNN.txt` — com a queima ligada haveria um arquivo
por bloco de legenda.

---

## 4. Nitidez: Short contra a fonte

Recorte do rosto, mesma cena, mesma região (conferida pela luma, que bate em 0,05),
normalizado para o mesmo tamanho.

**Short gerado agora contra o `video.mp4` que baixamos** (195×244, a resolução real da
fonte):

| | laplaciano |
|---|---|
| fonte 720p | 125,40 |
| Short 1080×1920 | 97,68 |

O pipeline perde **22%** do detalhe da fonte — encode + reamostragem. É a perda que está ao
nosso alcance.

**As duas capturas do dono** (`docs/mockups/rodape/`), com o mesmo recorte de rosto em cada,
normalizadas para 202×252:

| | laplaciano |
|---|---|
| captura do YouTube | 85,28 |
| captura do Short | 47,06 |

Mesma direção do 12,4 contra 55,6 medido pelo dono; os números absolutos diferem porque o
recorte e o tamanho de normalização não são os mesmos (a ferramenta é outra). O que se
compara entre as duas medições é a **razão**: 0,55 aqui, 0,22 lá.

Comparação visual: `docs/medicoes/rosto_pipeline_vs_youtube.png` (esquerda pipeline,
direita YouTube).

**O que a diferença diz.** A queda de 125 para 98 é nossa (22%). A queda de 85 para 47 na
comparação do dono é maior porque a captura do YouTube vem do **player**, que naquele momento
serviu uma imagem melhor do que os 1026k que o `yt-dlp` baixa — e, medido no item 3, não há
variante melhor para baixar. Ou seja: o teto de qualidade é a transmissão da igreja, e o que
sobra de recuperável no nosso lado são os ~22% de encode/reamostragem, não os 78%.
