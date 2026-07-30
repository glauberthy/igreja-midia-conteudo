# Redesenho da tela de revisão (v4) — desenho para revisão

> Nada implementado. Medições reais, feitas hoje (2026-07-30), no culto `fZGyLBofmmo`.
> As decisões que são do dono estão isoladas no fim.

## 1. A medição que você pediu: o overshoot da parada

**Método** (declarado antes de rodar): Chrome **headless** — sem janela na área de trabalho,
sem captura de tela, sem display. Uma página local carrega a IFrame API com o culto real, pede
parada em T, lê onde o player de fato parou, 10 vezes por mecanismo, e envia o resultado a um
servidor local. A regra 7 segue valendo para o que precisa do ouvido do dono.

| mecanismo | overshoot (ms) | mediana |
|---|---|---|
| **parada ANTIGA** (poll 200 ms, sem folga) | +77 +77 +78 +79 +79 +99 +100 +100 +100 +101 | **+89** |
| **parada ATUAL** (poll 40 ms, folga 40 ms para dentro) | −37 −33 −29 −24 −23 −20 −20 −12 −7 +3 | **−22** |

**Confirma o mecanismo e refuta a magnitude.** O overshoot existia (+89 ms, viés sempre
positivo: o operador ouvia mais do que o arquivo teria) e o conserto de hoje o inverteu para
dentro (−22 ms, a escuta agora é conservadora). Mas **89 ms não explica "faltou ~1 s"** — é uma
ordem de grandeza abaixo do sintoma.

**Limitação honesta:** o erro do `seekTo` mediu **0 ms em 20/20**, e essa parte é
**inconclusiva** — a medida lê o `currentTime` do próprio player, e se ele salta para um
keyframe, o relógio salta junto. O salto documentado na API existiria sem aparecer aqui. Só uma
referência externa (o áudio do arquivo) pega isso — e é o que o desenho passa a usar.

## 2. A sua rodada de 14:21:57 mostra a causa real, e ela é maior

Essa rodada é **posterior** aos consertos de hoje (13:31) e tem histórico completo. Todos os
deltas em zero — o sistema aplicou exatamente o que foi pedido:

```
seq 1  frase-inicio  pediu 5361000  aplicou 5361000  delta 0   48,00s
seq 2  frase-fim     pediu 5405000  aplicou 5405000  delta 0   44,00s
seq 3-7 fino-fim     5405250 … 5406250 (cinco empurrões de 0,25s), todos delta 0
seq 8  fino-fim      pediu 5406000  aplicou 5406000  delta 0   45,00s
seq 9  decisao-aprovado                                        45,00s
```

O arquivo saiu com **45,000 s exatos**. E o áudio, medido com `silencedetect` (−32 dB, pausa
≥ 0,30 s), diz onde a fala realmente está:

```
… 01:30:03.659  fala PARA      <- fim de "capaz de arrumar o caos."
   01:30:04.126  fala VOLTA
   [01:30:06.000  <- AQUI o corte termina ]
   01:30:09.486  fala PARA      <- fim de "O Senhor traga ordem na minha vida"
```

**O corte cai no meio de uma fala corrida de 5,4 s**, 3,5 s antes da pausa. Na transcrição:

```
[01:30:05] capaz de arrumar o caos. O Senhor traga
[01:30:07] ordem na minha vida. Traga vida para …
```

### O mecanismo, e ele não é o overshoot

O clique em frase termina o corte no **início do bloco de legenda seguinte**
(`fimDaFraseSeguinte`). Os blocos da legenda *rolling* quebram por largura de tela, **não** por
fim de fala: "…O Senhor traga" | "ordem na minha vida…". Ou seja, **o ponto que o sistema
oferece não é fronteira de FALA, é fronteira de LEGENDA** — e frequentemente cai no meio da
frase falada, às vezes no meio da palavra.

Depois disso, os empurrões de 0,25 s não têm alcance: para sair de 01:30:06 e chegar à pausa em
01:30:09,5 são **14 cliques**. Você deu 6 e voltou um.

**Conclusão que muda o desenho:** o preview local é necessário (elimina a discrepância entre
fontes por construção), mas **não é suficiente**. Sem uma fronteira vinda do ÁUDIO, o operador
continua sendo levado a pontos que a legenda inventou.

## 3. Acréscimo ao seu desenho: fronteiras de fala vindas do áudio

Uma passada de `silencedetect` no culto inteiro, **medida agora**:

| medida | valor |
|---|---|
| tempo para 1h50 de áudio (902 MB) | **6,5 s** |
| pausas encontradas (≥ 0,30 s, −32 dB) | **1835** |
| onde fica | `videos/<idVídeo>/pausas.json` (~30 KB), ao lado do vídeo, no cache |
| quando roda | uma vez por culto, junto do download |

Três usos, todos diretos:

1. **Encaixe do clique em frase passa a ser na pausa real** — "termina onde a fala termina", em
   vez de "onde a legenda troca de bloco". Resolve a classe do problema que você relatou.
2. **Marcas visíveis na régua ampliada** — o operador VÊ onde o pregador respira.
3. **Ímã no arraste** (dentro de ~200 ms), para o marcador cair na pausa sem precisão de pixel.

Isto **não é a Rota D**: não há alinhamento por palavra, não há modelo. É detecção de energia,
determinística, 6,5 s. A Rota D continua sendo a saída para *legenda* precisa (e para a spec-12);
o que isto resolve é *corte* preciso, que é o problema de hoje.

## 4. O desenho da tela

```
┌─ RÉGUA GERAL (o culto todo, ou a janela da pregação) ──────────────────────┐
│▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏[▓▓]▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏│
│                                  ^ o trecho, na posição dele              │
└───────────────────────────────────────────────────────────────────────────┘
        clique salta · sem arraste fino (ver "régua de simplicidade")

┌─ RÉGUA AMPLIADA (±60 s em volta do trecho ≈ 0,12 s/px) ────────────────────┐
│ ▁▂▃▅▂▁ ▁▃▅▆▅▃▁  ▁▂▅▇▅▂▁ ▁▃▅▂▁    ▁▂▃▅▇▆▃▁ ▁▂▁   ← onda do áudio (PNG)     │
│ ·   ·    ·   ·     ·  ·     ·  ·   ·    ·      ← pausas (marcas)          │
│         ┃▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓┃                                      │
│         ^ marcador início        ^ marcador fim   (arrastáveis)           │
│         └──── faixa 30–58 s desenhada ────┘                               │
│ 01:29:21.000                              01:30:09.486   dur 48,49 s      │
└───────────────────────────────────────────────────────────────────────────┘

<video src="/video/<idVídeo>" controls>   ← o MESMO arquivo que o corte usa
faixa de frases (fica como é)      empurrões ±0,25s / ±1s (ficam)
```

### 4.1 Fonte local — e o range já funciona

Rota nova `GET /video/{videoID}` servindo `videos/<id>/video.mp4` com `http.ServeFile`, o
**mesmo helper** do download dos Shorts. Verificado hoje contra o arquivo real de 902 MB:

```
HEAD  -> 200, Accept-Ranges: bytes, Content-Length: 944887249
range -> 206 Partial Content, Content-Range: bytes 800000000-800000099/944887249
         transferiu 100 bytes
```

Então o `<video>` faz seek em 01:30:00 pedindo só a faixa de bytes daquele ponto. **Não é preciso
fazer nada** além de criar a rota — com a mesma whitelist de sempre (id validado, `filepath.Base`).

Sai a IFrame API inteira: `criarPlayer`, `onYouTubeIframeAPIReady`, `prontoPlayer`, `seekTo`,
`getCurrentTime`, `setPlaybackRate`, o `<script src="youtube.com/iframe_api">` e o polling de
parada. São ~80 linhas de JS a menos, e uma dependência externa a menos na tela.

### 4.2 Régua em dois níveis

- **Geral**: o culto (ou a janela da pregação) em ~1000 px. Serve para **contexto e salto**.
- **Ampliada**: ±60 s em volta do trecho ⇒ 120 s / 1000 px = **0,12 s/px**. Um pixel de arraste
  vale menos que meio quadro; a resolução de ±0,25 s pedida sobra (2 px).

Quando o trecho é maior que 120 s a ampliada passa a ser `duração + 30 s` de cada lado, para o
trecho nunca sair da vista.

### 4.3 Posicionar-e-ler

O valor do corte passa a vir de **onde o operador parou**, lido do `currentTime` do `<video>` —
não do instante em que uma parada automática disparou. Botões `usar como início` / `usar como
fim` sobre a posição atual, e o arraste do marcador atualiza o `currentTime` em tempo real
(arrasta e vê/ouve).

As emendas continuam ("ouvir 5 s antes do fim"), agora com parada por `requestAnimationFrame`
sobre um relógio local — erro de 1 quadro (~17 ms) contra os 89 ms medidos do YouTube. E como o
número não vem mais da parada, o overshoot deixa de decidir qualquer coisa.

### 4.4 O que fica exatamente como está

- **Faixa de frases**: navega por CONTEÚDO ("começa nesta frase"). É complementar à régua, que
  navega por tempo. Muda apenas o encaixe do fim: pausa real em vez de bloco de legenda.
- **Empurrões ±0,25 s e ±1 s** sobre o mesmo valor.
- **Guardas no servidor** (faixa 30–58 s, clamp na janela da pregação, revalidação no `/aprovar`).
- **Histórico de ações** — e ele ganha os tipos novos (`arraste-inicio`, `arraste-fim`,
  `posicao-como-fim`), continuando a registrar pedido/aplicado.

### 4.5 Camadas na régua ampliada

| camada | custo medido | valor |
|---|---|---|
| **pausas do áudio** (marcas) | 6,5 s por culto (uma vez) | **alto** — é a decisão que o operador está tomando |
| **onda do áudio** (PNG do servidor, `showwavespic`) | **0,28 s** por janela, **1,9 KB** | **alto** — ele VÊ onde a fala termina |
| miniaturas (sprite, `fps=1/5,tile=24x1`) | 0,66 s por janela, 34 KB | **baixo** para este problema |

**Recomendo pausas + onda, e deixar as miniaturas fora da primeira versão.** O problema é de
áudio: onde a fala termina. Miniatura mostra cena, não fim de frase — na referência que você
mandou ela serve para achar o momento num clipe de 15 s, e aqui isso é o papel da faixa de frases
e da régua geral. Se depois você sentir falta, é um `ffmpeg` e um `background-position`.

### 4.6 Travas visíveis

A faixa 30–58 s desenhada na régua (sombreado do mínimo, borda do máximo) e a duração escrita ao
lado, para ele ver o limite **antes** de esbarrar. Hoje ele descobre ao tentar aprovar.

## 5. Régua de simplicidade — onde eu uso MENOS maquinaria

Você pediu para dizer onde dá para resolver com menos. Quatro lugares:

1. **A régua geral não tem arraste.** É uma `<div>` com uma barra de posição e um bloco marcando
   o trecho; clique salta. Todo o arraste fino mora na ampliada. Metade dos eventos.
2. **Nada de canvas.** A onda é PNG gerado pelo servidor (1,9 KB, 0,28 s) e entra como
   `background-image`. As pausas são `<div>`s absolutas — na janela de ±60 s são ~40, não 1835.
3. **Um único container de arraste**, com `setPointerCapture` no marcador. Dois marcadores, um
   handler de `pointermove`, uma função `pxParaMs`/`msParaPx`. Estimo **~120 linhas** de JS —
   menos do que as ~80 que SAEM com a IFrame API mais o polling de parada. **A tela fica com
   menos JS do que tem hoje.**
4. **Nenhum pacote novo no Go.** A rota é `http.ServeFile`; as pausas são um `[]int` em JSON
   dentro do `videocache` (mesmo lugar do `video.json`); a onda é um `exec` de ffmpeg no
   `internal/video`, ao lado dos outros.

O que eu **não** faria: reimplementar controles de player (o `<video controls>` nativo já dá
play/pause/seek/volume), nem sincronizar dois relógios (o `<video>` é a única fonte de tempo).

## 6. Fluxo: download antes da revisão, com números reais

Médias medidas em `resultados/tempos.csv` (46 pedidos concluídos):

| etapa | média | min | max |
|---|---|---|---|
| baixar legenda | 2,9 s | 2,5 | 3,8 |
| selecionar | 29,4 s | 20,6 | 38,9 |
| baixar vídeo | 62,9 s | 7,4 | 202,2 |
| renderizar | 9,0 s | 2,6 | 26,8 |

| momento | hoje | com o desenho | com cache (31 de 46 pedidos) |
|---|---|---|---|
| antes de revisar | ~32 s | ~32 s **+ 63 s** | ~32 s |
| depois de aprovar | ~72 s | ~9 s | ~9 s |
| total de máquina | ~104 s | ~104 s | ~41 s |

O custo **se redistribui**: o mesmo total, deslocado para antes da revisão. E em **2 de 3
pedidos** o vídeo já está no cache, onde não há nada a esperar. O caso ruim é o culto novo em
rede ruim (202 s medidos uma vez) — aí o operador espera antes de revisar. Mitigação simples:
começar o download **em paralelo com a seleção** (são independentes: um usa rede, o outro a GPU),
o que esconde os 63 s atrás dos 29 s da seleção e reduz a espera a ~34 s.

## 7. O que muda no que já está implementado

| arquivo | mudança | tamanho |
|---|---|---|
| `internal/servidor/servidor.go` | rota `GET /video/{videoID}`; rota da onda | ~30 linhas |
| `internal/servidor/templates.html` | sai IFrame API + polling; entram réguas e marcadores | −80 / +200 linhas |
| `internal/servidor/ajuste.go` | `encaixarFim`/`encaixarInicio` passam a usar as PAUSAS | ~40 linhas |
| `internal/videocache/` | `pausas.json` (gerar, ler, invalidar com o vídeo) | ~60 linhas |
| `internal/video/` | `Pausas()` (silencedetect) e `Onda()` (showwavespic) | ~70 linhas |
| `internal/servidor/acoes.go` | tipos novos de ação (arraste, posição) | 3 linhas |
| fase pesada / fase leve | download do vídeo antes da revisão (em paralelo com a seleção) | ~40 linhas |

## 8. O que muda na spec-05

- **v4 nova**: a tela de revisão passa a ter fonte local e régua em dois níveis; a IFrame API sai.
  Registrar as medições desta rodada (overshoot +89 → −22 ms; a fronteira de legenda contra a de
  fala; range 206 confirmado).
- **spec-05 v2** (ajuste manual): a seção do encaixe muda — `fimDaFraseSeguinte` deixa de ser a
  fronteira do corte. O texto atual justifica o encaixe assimétrico "porque a legenda adianta"; a
  medição de hoje mostra que o problema é mais fundo (o bloco quebra no meio da frase), e a
  correção é a pausa do áudio.
- **spec-12** (legenda queimada, suspensa): não muda, mas ganha um dado — a legenda está errada
  em *fronteira*, não só em *deslocamento*, o que reforça a Rota D para a legenda queimada.
- **spec-06**: `pausas.json` e a onda entram na lista de preservados do cache (derivados baratos:
  6,5 s e 0,28 s; podem ser regenerados, então podem ser removíveis também — decidir na
  implementação, com a régua de "o que custa caro recuperar").

## 9. Custo total

| item | esforço | risco |
|---|---|---|
| rota do vídeo local + range | pequeno | baixo (helper já usado) |
| trocar player YouTube por `<video>` | médio | **médio** — é a tela central; precisa do seu olho |
| réguas + marcadores | médio | médio (DOM/eventos; testável por referência de JS, não por aparência) |
| pausas do áudio + encaixe novo | pequeno | baixo (determinístico, testável) |
| onda | pequeno | baixo |
| download antes da revisão | pequeno | baixo |

Sugestão de ordem, cada fatia verificável sozinha: **(1)** pausas + encaixe novo (resolve o bug
relatado sem tocar na tela) → **(2)** rota local + `<video>` (elimina a discrepância de fontes) →
**(3)** réguas e marcadores → **(4)** onda → **(5)** fluxo do download.

Vale dizer: **a fatia (1) já resolve o caso que você relatou hoje**, e não depende de nada visual.

## 10. Decisões que são suas

1. **Miniaturas na v1?** Eu deixaria fora (o problema é de áudio; custo 0,66 s e 34 KB por
   janela). A onda entra no lugar.
2. **Ímã nas pausas: por padrão ou com tecla?** Sugiro por padrão dentro de 200 ms, com
   `Shift` para ignorar — mas é o seu ouvido que decide se o ímã ajuda ou incomoda.
3. **Encaixe do clique em frase: pausa real ou pausa OU legenda, o que vier depois?** Sugiro a
   pausa real, sempre. É a mudança que resolve o bug; e a legenda deixa de ter voto no corte.
4. **Download antes da revisão: em série ou em paralelo com a seleção?** Sugiro paralelo (esconde
   63 s atrás de 29 s). Em série é mais simples de ler no log.
5. **Régua geral: o culto inteiro ou a janela da pregação?** Sugiro a janela (é o que o operador
   pediu; o resto do culto não é material dele).
