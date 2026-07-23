# Spec 12 — Legenda queimada: texto limpo, 1–2 linhas, posicionada acima da logo

## Objetivo

Corrigir a legenda queimada nos Shorts. Hoje ela é o SRT bruto do YouTube: 4 linhas
"rolling" acumuladas, cobrindo o peito e as mãos do pregador, com pontuação quebrada
("se lembre. de alguém"), marcadores ">>" e duplicações. Passa a ser: **texto limpo,
1–2 linhas por vez, na base do vídeo (acima da logo), em Google Sans Flex encorpada,
branca com contorno** — legível sem tapar o pregador.

## Diagnóstico (confirmado por frame real)

Frame do culto-noite: legenda com 4 linhas centralizada no meio vertical, cobrindo
peito/mãos do pregador (o rosto fica livre, mas as mãos, que são expressivas, não).
Branca com contorno preto (contraste OK). Texto sujo: "se lembre. de alguém" (pontuação
quebrada da rolagem). A logo do rodapé AINDA NÃO está integrada ao render (será integrada
junto — ver spec da logo / seção de layout).

Causas: (1) render queima o SRT bruto rolling (4 linhas acumuladas) em vez de texto limpo;
(2) posição centralizada vertical em vez de base; (3) texto não passa pela limpeza que a
Fase 3 já sabe fazer (`Frasear`).

## Decisões (não reabrir)

- Legenda na **base do vídeo**, acima da logo (a logo fica no rodapé, centralizada,
  discreta; a legenda logo acima dela). Nunca no meio da tela.
- **1–2 linhas por vez**, nunca 4. Blocos curtos de texto, trocando conforme a fala.
- **Texto limpo**: sem rolagem/duplicação, sem ">>", pontuação normalizada. Reusar a
  lógica de desduplicação/`Frasear` da Fase 3 (não reimplementar).
- Fonte **Google Sans Flex** (mesma família da logo — identidade coesa), peso encorpado
  (Bold ou ExtraBold), variante óptica de display (`_72pt` ou `_36pt`). Branca com
  contorno preto (mantém o contraste que já funciona; resolve branco-sobre-fundo-claro).
- O render aponta para o arquivo `.ttf` da fonte diretamente (não depende de instalar no
  sistema). Os recursos visuais ficam todos em **`./assets`**: a logo em
  `assets/logo_ibi_gsf.png` (e `@2x`), e as fontes em **`assets/fontes/`** (ex.:
  `assets/fontes/static/GoogleSansFlex_72pt-Bold.ttf` ou `_36pt-Bold.ttf`; há também a
  variable font e os pesos de Thin a Black). O caminho do `.ttf` usado deve ser
  configurável (flag/constante), com default apontando para o peso encorpado escolhido em
  `assets/fontes/`.

## Escopo

Dentro:
- `internal/video` / `cmd/render`: gerar a legenda queimada a partir do TEXTO LIMPO do
  trecho (não do SRT bruto), segmentada em blocos de 1–2 linhas com tempos, posicionada
  na base (acima da reserva da logo).
- Estilo: fonte Google Sans Flex (caminho do `.ttf` configurável por flag/constante),
  peso encorpado, tamanho calibrado para 1080×1920, branca com contorno/sombra, largura
  máxima que force no máximo ~2 linhas.
- Reaproveitar a desduplicação/segmentação já existente (Fase 3 / `Frasear`) para o texto
  da legenda — mesma fonte de verdade do texto, evitando a duplicação de lógica que a
  gente já tinha anotado como pendência.
- Reservar a faixa inferior para a logo (a legenda não invade essa faixa).

Fora:
- Integração da logo em si (imagem no rodapé) — se ainda não existe, tratar junto ou em
  spec irmã; esta spec assume a faixa da logo reservada e posiciona a legenda acima dela.
- Timestamp preciso por palavra (é a Rota D / Whisper, futura — ver `ideias-futuras.md`).
  A legenda herda o mesmo timestamp aproximado da legenda do YouTube; o objetivo aqui é
  visual (limpeza, posição, quantidade), não sincronia sub-segundo.

## Layout vertical (de baixo para cima)

1. Rodapé: **logo** da igreja (centralizada, discreta).
2. Acima da logo: **legenda** (1–2 linhas), com margem que não encosta na logo.
3. Restante: o pregador (rosto e, idealmente, mãos livres).

## Questões em aberto (decidir na execução, registrar)

- Tamanho exato da fonte e largura máxima da caixa: calibrar vendo o resultado (começar
  com algo que force ≤2 linhas e deixe o pregador visível).
- Peso exato (Bold vs ExtraBold) e variante óptica (36pt vs 72pt): testar no vídeo real.
- Quantas palavras/segundos por bloco de legenda (ritmo de troca): calibrar para leitura
  confortável sem virar "rolagem" de novo.

## Critérios de aceite

- [ ] A legenda queimada usa TEXTO LIMPO (sem rolagem, sem ">>", sem duplicação,
      pontuação normalizada), vindo da mesma lógica da Fase 3.
- [ ] Máximo de 2 linhas por vez; nunca 4.
- [ ] Posicionada na base, acima da faixa reservada à logo; não cobre o rosto nem (o
      máximo possível) as mãos do pregador.
- [ ] Fonte Google Sans Flex encorpada, a partir do `.ttf` apontado diretamente; branca
      com contorno/sombra legível sobre fundo claro e escuro.
- [ ] Teste visual no culto-noite: comparar um frame antes (4 linhas no meio) e depois
      (≤2 linhas na base) — o pregador fica visível.
- [ ] `go build ./...` e `go test ./...` verdes (o que for testável; o visual é conferido
      pelo operador no vídeo real).

## Como validar

```bash
go run ./cmd/render -id culto-noite-19-07-26 -margem-fim 0
# abrir um short e conferir: legenda na base, <=2 linhas, texto limpo, pregador visível.
# extrair um frame para comparar:
ffmpeg -y -ss 2 -i finalizados/culto-noite-19-07-26/short_01.mp4 -frames:v 1 -update 1 /tmp/frame_legenda.png
```

## Nota

Esta spec trata a legenda; a logo no rodapé precisa ser integrada ao render (não está
hoje). Se a integração da logo não for feita nesta spec, reservar a faixa inferior e
posicionar a legenda acima dela, para não precisar refazer quando a logo entrar.
