# Spec 13 — Logo da igreja no rodapé do Short

## Objetivo

Integrar a logo da igreja ao render, sobreposta no rodapé de cada Short, centralizada e
discreta, na faixa inferior que a legenda (spec-12) já reserva. Marca a identidade da
igreja sem competir com a legenda nem cobrir o pregador.

## Contexto

A legenda (spec-12) já reserva a faixa inferior (`faixaLogoPx = 240`) e se posiciona
acima dela. Falta colocar a logo nessa faixa. A logo já existe, pronta, em
`assets/logo_ibi_gsf.png` (e `assets/logo_ibi_gsf@2x.png`): símbolo IBI verde-limão +
nome "PRIMEIRA IGREJA BATISTA NA VILA DO IPSEP" em Google Sans Flex, texto branco, fundo
transparente.

## Decisões (não reabrir)

- Logo no **rodapé, centralizada horizontalmente**, dentro da faixa reservada (≤240px de
  altura). Discreta — pequena o bastante para não roubar atenção, visível o bastante para
  marcar a igreja.
- **Permanente**: aparece o vídeo inteiro (é selo de marca, não troca como a legenda).
- Usa o PNG transparente de `assets/` diretamente (sobreposição, não regenera a logo).
- Tamanho e posição vertical **calibráveis** (flag/constante), como foi feito com a
  legenda — o valor exato se acha vendo o resultado.

## Ponto de atenção — texto branco em fundo claro

A logo tem texto BRANCO. Nos frames do culto-noite, o rodapé do vídeo é CLARO (chão de
madeira/base bege), então o texto branco da logo pode sumir parcialmente. Já sabíamos
desse risco (o usuário aceitou porque o rodapé "costuma" ser escuro), mas este vídeo
mostra que nem sempre é. Opções (decidir na execução, vendo o resultado):
- (a) adicionar uma sombra/contorno sutil à logo na composição (como a legenda tem),
  garantindo leitura em qualquer fundo — recomendado;
- (b) uma faixa/gradiente escuro semitransparente atrás do rodapé (também ajudaria a
  legenda a se destacar);
- (c) aceitar o risco como está (só se o teste visual mostrar que fica ok).
Testar e escolher vendo o frame.

**Resolvido: (b), gradiente.** O histórico dos valores e o porquê de cada troca está na
seção seguinte — é o parâmetro visual que mais mexeu desde então.

## Gradiente do rodapé — valores e por que mudaram

O gradiente é preto, transparente em cima e escuro embaixo, com a opacidade subindo por uma
curva (`pow`, expoente 2,2) para o topo ser imperceptível. Dois parâmetros: **altura** da
rampa (px, de baixo para cima) e **opacidade máxima** (na base). Eles **não se julgam
separados** — 0,60 em 520 px escurece o rodapé muito mais que 0,60 em 1500 px, porque a
curva sobe no mesmo espaço.

| quando | valor | por quê |
|---|---|---|
| início | 1200 / 1,00 | primeiro palpite; base escura demais |
| spec-13 | **1500 / 0,72** | escolha do operador entre quatro variantes: rampa mais alta e menos opaca, degradê mais gradual |
| 2026-07-29 | **520 / 0,60** | a legenda foi suspensa (spec-12) e o gradiente passou a servir só a logo |

### A troca de 2026-07-29 (1500/0,72 → 520/0,60)

O gradiente servia a **dois** fins: contraste da legenda e legibilidade da logo branca. Com
a legenda suspensa sobrou a logo, que ocupa os **240 px** de baixo — e o gradiente cobria
**1500 px, 78% da altura do Short**. Era isso, e não a legenda, que causava a reclamação de
"apertado".

Nove variantes renderizadas pelo caminho real (`cmd/render` com
`-rodape-altura/-rodape-escuro/-faixa-logo`) no mesmo frame, com o pregador de **camisa
branca** — o pior caso para texto branco. Duas grandezas, porque o trade-off tem dois lados:

- **torso** (luma média do recorte 1080×700 em y=1000): maior = mais imagem do pregador
  preservada. Teto, sem gradiente nenhum: **122,33**.
- **sob o texto da logo** (luma média do recorte 330×158 na altura do texto, renderizado
  *sem* a logo para medir só o fundo): menor = mais contraste para o branco. Sem gradiente:
  **166,06**, e aí o texto quase desaparece na camisa.

| variante | torso ↑ | sob o texto ↓ |
|---|---|---|
| 1500 / 0,72 (antes) | 87,96 | 68,08 |
| 700 / 0,65 | 114,00 | 97,65 |
| **520 / 0,60 (escolhida)** | **119,03** | **113,29** |
| 420 / 0,90 | 120,16 | 99,64 |
| 480 / 1,00 | 118,11 | 83,04 |
| 420 / 0,55 (faixa 200) | 121,04 | 106,16 |
| 340 / 0,72 (faixa 180) | 121,82 | 93,51 |
| 500 / 0,80 (faixa 300) | 118,44 | 123,02 |
| sem gradiente | 122,33 | 166,06 |

**O trade-off que o operador escolheu, explícito.** A medição favorecia 420/0,90; o operador
preferiu **520/0,60**, mais suave e mais clara. O que se troca:

- **a favor**: rampa mais longa e menos opaca dá um degradê mais gradual, e devolve
  praticamente a mesma imagem do pregador (119,03 contra 120,16 — 1,1 ponto de luma, dentro
  do imperceptível; as duas recuperam ~90% do que 1500/0,72 escurecia);
- **contra**: **13 pontos menos de contraste sob o texto branco** da logo (113,29 contra
  99,64). Ainda muito melhor que sem gradiente (166,06) e bem mais claro que os 68,08 de
  antes — a logo continua legível na camisa branca, com menos folga.

Ou seja: a escolha custa legibilidade da marca, não imagem do pregador. É decisão de
aparência, e é do operador. Se a logo se mostrar apagada num culto de rodapé claro, o
caminho é `420/0,90` (mesma imagem, +13 de contraste) ou `480/1,00` (−1 de imagem, +30 de
contraste), sem recompilar: `-rodape-altura`/`-rodape-escuro`.

**A faixa da logo NÃO mudou** (segue 240 px). Descê-la melhora as duas medidas (variantes de
faixa 180/200 acima, que põem o texto na parte escura da rampa), mas com faixa 180 a logo
fica a 11 px da borda inferior, dentro da área em que o player do Shorts desenha barra de
progresso e descrição.

Medições completas, método e ferramenta: `docs/medicoes/imagem-sem-legenda.md`.
Frames: `docs/mockups/rodape-sem-legenda/`.

## Escopo

Dentro:
- `internal/video` / `cmd/render`: sobrepor `assets/logo_ibi_gsf.png` no rodapé,
  centralizada, na faixa reservada, o vídeo inteiro, via ffmpeg (overlay do PNG com
  alpha). Caminho da logo e tamanho/posição configuráveis (flag/constante), default
  apontando para `assets/`.
- Garantir que a logo não colida com a legenda (a legenda fica ACIMA da faixa da logo; a
  logo DENTRO dela). Rever o valor de `faixaLogoPx` se, no teste, a logo e a legenda se
  tocarem.
- Tratar o texto-branco-em-fundo-claro conforme o ponto de atenção (provável: sombra/
  contorno sutil na composição, ou faixa escura).

Fora:
- Redesenho da logo (ela já está pronta).
- Ajuste fino de legenda (spec-12, já feita).

## Critérios de aceite

- [ ] A logo aparece no rodapé, centralizada, o vídeo inteiro, em todos os Shorts.
- [ ] A logo não cobre o pregador nem colide com a legenda (legenda acima, logo abaixo).
- [ ] A logo permanece legível mesmo sobre rodapé claro (via sombra/contorno/faixa, ou
      confirmado visualmente que fica ok sem).
- [ ] Caminho da logo e tamanho/posição configuráveis; default em `assets/`.
- [ ] Teste visual no culto-noite: frame mostra logo + legenda + pregador coexistindo bem.
- [ ] `go build ./...` e `go test ./...` verdes (o testável; o visual é conferido no frame).

## Como validar

```bash
ID=culto-noite-19-07-26
go run ./cmd/render -id "$ID" -margem-fim 0
ffmpeg -y -ss 3 -i "finalizados/$ID/short_01.mp4" -frames:v 1 -update 1 frames-teste/frame_logo.png
# conferir: logo no rodapé centralizada, legível, sem colidir com a legenda nem cobrir o pregador.
```

## Nota

Com a legenda (spec-12) e a logo (spec-13), o rodapé do Short fica completo: logo no
fundo, legenda acima. É o último elemento visual do produto — depois disto, o Short está
visualmente pronto para publicar.
