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
