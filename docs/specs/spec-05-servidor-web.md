# Spec 05 — Interface web do operador (fluxo invertido: selecionar antes de baixar)

> **Desenho atual: v3** (2026-07-29) — quatro telas navegáveis no cliente e cache de vídeo
> por culto. Ver a seção **"v3 — quatro telas navegáveis + cache por vídeo"**. As seções v1 e
> v2 continuam aqui como registro do que foi decidido e medido; onde a v3 muda uma decisão, a
> decisão antiga está marcada como superada, não apagada.
>
> O rascunho da v3, com as alternativas descartadas e as decisões antes de serem tomadas, está
> em `docs/plano-spec-05-v3.md`. Ele é histórico; **onde os dois divergirem, vale esta spec.**

## Objetivo

Uma interface web local para o operador leigo (pastor/auxiliar) gerar os Shorts sem usar
o terminal. O fluxo é **invertido** em relação ao pipeline de linha de comando: primeiro
seleciona (usando só a legenda, leve), o operador **pré-visualiza e aprova** os trechos
dentro da própria página (player do YouTube embutido), e **só então** o vídeo é baixado e
os trechos aprovados são cortados. Economiza banda/tempo e põe a revisão humana no ponto
certo, antes do processamento pesado.

## Contexto e decisões de produto

- O operador não usa terminal. Abre o navegador numa porta local, cola o link do YouTube
  e os tempos da pregação, acompanha, revisa e aprova. Pega os arquivos de finalizados/
  e envia pelo WhatsApp Web manualmente (sem integração de mensageria).
- **Fluxo invertido** (decidido em ideias-futuras.md): a seleção é 100% texto (legenda),
  então é barato selecionar antes de baixar o vídeo inteiro. Só o aprovado é baixado.
- ~~**Uma tela** conduzindo todas as etapas (decisão do dono)~~ — **SUPERADO pela v3**: são
  **quatro telas navegáveis** (dados → processando → revisão → resultado), com indicador de
  etapa. O que a decisão original queria dizer continua valendo: uma única PÁGINA, sem
  recarregar, conduzindo o operador do link ao Short. A v3 mantém isso — as quatro telas
  ficam no mesmo documento e a troca é local. Ver a seção da v3.
- **v1 = aprovar/reprovar SEM ajuste fino de corte** (decisao do dono). O ajuste manual
  (marcar inicio/fim ouvindo, via IFrame API) veio na **v2, IMPLEMENTADA** -- ver a secao
  "v2 -- ajuste manual do corte pelo operador" no fim desta spec.
- Um pedido por vez, fila simples (2 operadores, uso esporadico).
- Sem autenticacao (uso local, rede confiavel). Porta dedicada, padrao :7799 (nunca
  80/8080/8000), configuravel.

- **Stack de front: HTMX + JavaScript vanilla (so para o player YouTube).** O servidor Go
  gera HTML (via embed) e o HTMX faz as atualizacoes assincronas por atributos
  (hx-get/hx-post/hx-trigger), sem framework, sem build, sem npm. Combina com o backend Go
  (logica no servidor, HTML pronto na resposta). O unico JS vanilla e o do player YouTube
  (IFrame API: onYouTubeIframeAPIReady, criar player, seekTo), que convive com o HTMX sem
  conflito. Evita over-engineering (React/Vue seria demais para uma tela local esporadica).
  Na v1, o polling de status usa hx-trigger="every 2s"; no FUTURO, trocar o polling por um
  endpoint SSE no servidor Go (HTMX como cliente do SSE) -- ver pendencias.

## v3 — quatro telas navegáveis + cache por vídeo (2026-07-29)

> Esta seção é o desenho ATUAL da interface e do armazenamento. O fluxo do pipeline
> (selecionar antes de baixar) não muda; muda a interface sobre ele e onde os arquivos moram.

### Por que agora

Três incômodos do uso real, e um problema de disco:

1. **Aperto.** `.folha { max-width: 720px }` com a revisão em duas colunas dentro. É a
   reclamação principal do operador.
2. **Sem navegação.** Não existe voltar: o operador vê a etapa que o servidor mandou, e só.
3. **Etapas invisíveis.** Baixar legenda (3 s) e selecionar (~32 s) apareciam como um
   "Processando… <estado>", sem dizer onde está.
4. **Cada pedido rebaixava tudo**, inclusive ~570 MB de vídeo, mesmo sendo o mesmo culto de
   meia hora antes. E a spec-06 APAGAVA o vídeo ao concluir — o oposto de um cache.

### As quatro telas

```
┌─ 1 dados ──┐  ┌─ 2 processando ─┐  ┌─ 3 revisão ─┐  ┌─ 4 resultado ─┐
│ link       │  │ legenda    ✓ 3s │  │ (a tela da  │  │ short_01 ▶ ⬇ 🗑 │
│ início/fim │→ │ seleção  ▓▓▓32s │→ │  v2, na     │→ │ short_02 ▶ ⬇ 🗑 │
│ [enviar]   │  │                 │  │  largura    │  │ short_03 ▶ ⬇ 🗑 │
└────────────┘  └─────────────────┘  │  toda)      │  └────────────────┘
                                     └─────────────┘
```

**Legenda e seleção numa etapa só** (decisão do dono): baixar legenda leva 3 s e não merece
tela própria. A tela "processando" mostra as duas linhas com o estado de cada uma e o tempo
decorrido — a seleção é a que custa ~32 s e é ela que precisa de sinal de vida.

O indicador de etapa é uma faixa de quatro botões, sempre visível. Estados: `atual`,
`alcançada` (clicável), `bloqueada` (desabilitada), `erro`.

> Cuidado de nome: já existe `.trilha` na revisão — é a régua de TRECHOS (um quadradinho por
> candidato). O indicador de ETAPAS é `#etapas` / `.etapas`. Nomes distintos de propósito,
> para ninguém reaproveitar o CSS errado.

### Navegação no cliente, ações no servidor (requisito do dono)

**As quatro telas ficam no DOM desde o primeiro carregamento.** Trocar de tela é `hidden`
para lá e para cá, com o estado num objeto do cliente. **Trocar de tela não gera requisição** —
o operador navega quantas vezes quiser durante a revisão sem esperar servidor.

| quem | o quê |
|---|---|
| **JS vanilla** | navegar, mostrar/esconder, indicador de etapa, player, ajuste de corte |
| **HTMX** | criar pedido, polling do progresso, ajustar corte, aprovar, gerar, apagar Short |

Estado do cliente — um objeto só, ao lado do `REV` que já existe (o `REV` continua sendo o
estado da revisão: trechos, ajustes, decisões; não muda):

```js
var APP = {
  tela: 'dados',
  pedidoId: null,
  alcancadas: { dados: true, processando: false, revisao: false, resultado: false },
  entrada: { url: '', inicio: '', fim: '' },  // reexibe a tela 1 sem pedir ao servidor
  shorts: [],
  sujo: false,   // há decisão de revisão não enviada
};
function irPara(tela) { /* só hidden + classes do indicador */ }
```

### O que "navegável" significa, por etapa

Dois verbos, e a diferença é a regra inteira: **ver** é livre; **refazer** destrói trabalho.

| ação | permitido | efeito |
|---|---|---|
| ver etapa já alcançada | sempre, sem confirmar | só mostra. Nada no servidor. `REV` intacto |
| ver `dados` durante a revisão | sim | campos preenchidos em modo leitura + botão de nova janela |
| ver `processando` depois de pronto | sim | mostra quanto cada etapa levou (vira registro, não spinner) |
| ver `revisão` a partir do resultado | sim | decisões visíveis, congeladas |
| avançar para etapa não alcançada | **não** | botão desabilitado; quem libera é o servidor (revisão só com candidatos; resultado só com Shorts) |
| **buscar outros trechos** | confirmação | descarta candidatos, aprovações e ajustes; re-roda a seleção com `HARNESS_TEMP > 0` |
| **nova janela** (outro início/fim) | confirmação | mesmo descarte; **novo pedido**, vídeo reaproveitado do cache |
| **gerar de novo** (do resultado) | confirmação | re-renderiza; sobrescreve os Shorts daquele pedido |

A confirmação **nomeia o que se perde**: "isso descarta 4 aprovações e 2 ajustes de corte".
Um "tem certeza?" genérico não informa nada.

**Não existe "refazer a seleção" avulso** (decisão do dono). Com o default `HARNESS_TEMP=0`,
re-rodar devolve os mesmos candidatos: seria uma espera de 32 s sem efeito. O botão é
**"buscar outros trechos"**, que re-roda com temperatura maior — exatamente o desenho já
registrado na seção "Temperatura padrão = 0". Quem quer outro resultado com temperatura 0
troca a janela.

### F5: reidratação a partir do servidor

Ao carregar (carregar não é navegar), **uma** requisição: `GET /pedido-atual` → `204` se não
há nada, ou o mesmo payload do `GET /pedidos/{id}`.

| status no servidor | tela ao abrir |
|---|---|
| nada em memória | `dados` |
| fase leve em curso | `processando`, polling religado |
| `aguardando-aprovacao` | `revisao`, reidratada do payload (o JSON dos trechos já vem lá, `RevisaoDados`) |
| fase pesada em curso | `processando` (bloco da fase pesada) |
| `concluido` | `resultado`, com a lista de Shorts |
| `erro` | a tela da etapa que falhou, com a mensagem |

**O que o F5 NÃO recupera: as decisões da revisão em andamento** (aprovado/reprovado e
ajustes ainda não enviados) — elas vivem só no `REV`. Decisão do dono: **avisar e aceitar a
perda** (`beforeunload` quando `APP.sujo`). Persistir decisão a cada clique transformaria a
revisão numa conversa constante com o servidor, o oposto do requisito; guardar rascunho no
`localStorage` criaria um segundo lugar com estado. Fica registrado como melhoria possível,
não como dívida.

O `-retomar <id>` continua sendo o caminho para trazer de volta pedido de OUTRA execução; a
reidratação cobre o pedido que o servidor tem em memória.

### Largura e respiro (decisão do dono)

- `.folha`: **720px → 1180px**. O rodapé fixo acompanha. 1180 dá ~560px por coluna na
  revisão; mais que isso começa a espalhar o olho em telas grandes.
- Escala de espaçamento em variáveis (`--esp-1: 8px` … `--esp-5: 32px`) substituindo os
  valores soltos: padding dos cartões 20/22 → 28, `gap` das colunas 18 → 28.
- Faixa de frases: `max-height` 420 → 560px (é onde a falta de espaço mais aparece).
- `@media (max-width: 760px)` continua (uma coluna) — o operador às vezes abre no celular.

### Tela de resultado

Um cartão por Short: `<video controls preload="metadata">` (arquivo local, não YouTube),
duração, tamanho, e três ações: **assistir** embutido, **baixar**, **apagar** (confirmação
nomeando o arquivo). Apagar remove só o arquivo — o registro em `cortes.csv` é medição do
desvio da legenda, não inventário de arquivos, e continua.

`DELETE /finalizados/{id}/{arquivo}` reusa a **mesma** validação de nome do
`handleBaixarFinal` (coberta por `TestBaixarFinalRecusaArquivoForaDaWhitelist`). Um endpoint
que APAGA com travessia de caminho é muito pior que um que baixa.

#### Compartilhar: a limitação, registrada com honestidade

Não há caminho bom, e a spec registra isso em vez de fingir que há:

- **integração com WhatsApp não existe** e é decisão registrada (envio é manual);
- **Web Share API com arquivo** (`navigator.share({files})`) é instável fora do celular: no
  desktop, ou não expõe o alvo certo, ou falha silenciosamente;
- **um botão que abrisse o WhatsApp sem anexar o vídeo seria pior que não ter** — promete o
  fluxo e entrega meia ação, e o operador só descobre no meio do envio.

O fluxo real, escrito na tela em uma linha: **baixar e enviar pelo WhatsApp Web.** Sem botão
falso.

### Cache por vídeo — dois níveis de armazenamento

Os artefatos têm naturezas diferentes, e é isso que define onde moram:

```
videos/<idDoVídeo>/          # imutável, reutilizável, PESADO (~570 MB)
  video.mp4
  video.json                 # {video_id, origem_ms, baixado_em, usado_em, bytes, titulo}
  legenda.srt
  legenda.info.json
  transcricao.txt            # do vídeo INTEIRO

trabalho/<idDoPedido>/       # depende da janela e das decisões; leve (KB)
  pedido.json                # {..., video_id}
  transcricao.txt            # RECORTADA à janela (derivada; é o que a seleção lê)
  candidatos.corrigido.json

finalizados/<idDoPedido>/short_NN.mp4
```

**Por que a transcrição aparece nos dois lugares:** hoje ela é recortada à janela no momento
do download (`internal/download/ytdlp.go`, com tempos ABSOLUTOS preservados). O cache tem de
ser do vídeo **inteiro** para servir qualquer janela; o recorte continua por pedido, porque é
o que a seleção e o `cmd/auditar` leem. É texto — recortar de novo é barato, e muito melhor
que mudar o contrato dos consumidores.

**O ID do vídeo NÃO nomeia a pasta do pedido.** Pedido segue `web-<timestamp>-<n>`. Duas
pregações na mesma transmissão (acontece), ou o operador refazendo com outra janela,
compartilham o vídeo e mantêm candidatos separados. Usar o id do vídeo como nome do pedido
faria o segundo sobrescrever os candidatos do primeiro.

**Sinergia com a decisão de baixar o vídeo inteiro:** como o download é do vídeo completo
(medido: 7,3 s contra 577 s da janela), o cache serve **qualquer** janela sem verificação de
cobertura. Se o download fosse por trecho, cada acerto de cache exigiria checar se a janela
pedida cabe no que está em disco — e essa checagem é exatamente o tipo de aritmética que já
produziu bug de origem aqui.

Fluxo:

```
POST /pedidos
 └─ extrai video_id da URL → grava em pedido.json
 └─ fase leve:  videos/<vid>/ completo?
      sim → reusa legenda + transcrição (0 s), toca usado_em
      não → baixa a legenda para o cache
    → recorta a transcrição à janela em trabalho/<id>/ → seleção
 └─ (aprovação humana)
 └─ fase pesada: videos/<vid>/video.mp4 existe?
      sim → PULA o download (~35 s e ~570 MB de banda)
      não → baixa o vídeo inteiro para o cache, grava video.json
    → render recebe (caminho, origem) do localizador
```

### A origem do vídeo mora AO LADO do vídeo (ligação com a spec-09)

Esta é a classe de bug que já custou duas rodadas, então o desenho é explícito:

> **Cada arquivo de vídeo carrega a própria declaração de origem, ao lado dele.**
> `videos/<id>/video.json` descreve `videos/<id>/video.mp4`.
> `pedido.json.origem_ms` descreve `trabalho/<id>/video.mp4` (fluxo do `cmd/baixar` por
> janela, que continua existindo).

Um localizador único — o **único** lugar que resolve "qual arquivo e qual origem":

```go
// internal/videocache
func Localizar(videosDir, baseDir string, ped *pipeline.Pedido) (path string, origemMs int, err error)
```

Regra em uma frase: **vídeo na pasta do pedido vence** (é o fluxo por janela, mais
específico); senão o cache; se nenhum dos dois, **erro claro dizendo o que falta**. Nunca
dedução — nem por duração de arquivo, nem por `ped.Inicio`. Quem chama: fase pesada do
servidor e `cmd/render`.

**Duas guardas por construção, decididas na implementação:**

1. **O `internal/video` PERDEU o acesso à origem.** `Renderizar` recebe `videoPath` e
   `origemMs` de fora e `RenderizarComOrigem` deixou de existir. O pedido era um teste que
   detectasse leitura fora do resolvedor; o teste existe (varredura de AST, com mutação
   verificada), mas a garantia forte é a outra: **não há de onde deduzir**. Teste detecta
   violação; remover o acesso torna a violação impossível.

2. **`videocache.Registrar` RECUSA origem != 0** (`videocache.Aceita`, `ErrOrigemNaoZero`).
   "O cache só contém vídeo inteiro" deixou de ser convenção e virou invariante do pacote,
   num lugar só, independente de quem chama.

   O motivo é uma diferença de categoria, não de grau. Um vídeo de **janela** registrado no
   cache teria `origem_ms: 0` — a origem declarada **mentindo sobre o conteúdo**, gravada em
   disco. O bug de corte deslocado que custou duas rodadas morria no fim da execução; este
   envenenaria **todo pedido futuro** que reusasse aquele culto, inclusive de outro sermão.
   A migração já checava isso antes de mover o arquivo, e era a checagem certa no lugar
   errado: protegia UM caminho, e qualquer via nova de escrita reabriria o furo. A migração
   agora consulta o mesmo `Aceita` — porque ela precisa perguntar ANTES do `rename` (descobrir
   a recusa depois deixaria o vídeo fora da pasta do pedido e sem índice no cache).

### Extração do ID do vídeo

Já existe e já cobre `/live/` (`internal/servidor/videoid.go` + teste). O que a v3 muda:

- **muda de lugar** para `internal/download` (exportada como `download.VideoID`), porque
  agora o download também precisa dela;
- **passa a ser validada com rigor** — `^[A-Za-z0-9_-]{11}$` — porque o valor deixa de ser
  parâmetro de iframe e passa a ser **nome de diretório**. Sem validar, uma URL hostil
  escolhe onde escrevemos; é a mesma preocupação do `retencao.caminhoSeguro`;
- casos de teste obrigatórios: `watch?v=`, `youtu.be/`, `&t=`, `&list=`, **`/live/<id>`** (é
  como o YouTube endereça transmissão ao vivo, o caso desta igreja),
  `/live/<id>?feature=share`, `/embed/`, `/shorts/`, `m.youtube.com`, host em maiúsculas, URL
  de outro site (rejeita) e entrada hostil como `../../etc` (rejeita).

### Fora do escopo da v3

- Persistir decisões da revisão (F5 recuperar aprovações) — decidido: avisar e aceitar.
- SSE em vez de polling — segue registrado em `docs/ideias-futuras.md`.
- Qualquer forma de compartilhamento automático.

## Fluxo (uma tela, etapas)

1. **Entrada**: operador informa {youtube_url, inicio, fim} da pregacao e envia.
2. **Fase leve (baixar-legenda + selecionar)**: o sistema baixa APENAS a legenda/
   transcricao (yt-dlp, sem o video) e roda o harness (selecao). A pagina faz polling do
   status (baixando-legenda, selecionando, validando).
   - Se nao houver legenda automatica (DP-001): a pagina informa e para, sem baixar video.
3. **Revisao**: a pagina lista os trechos candidatos. Para cada um:
   - um **player do YouTube embutido** (IFrame Player API) que toca o trecho de start a
     end (via seekTo + parar no fim), para o operador assistir/ouvir dentro da pagina;
   - o hook, a duracao e o score;
   - se requer_revisao_reforcada = true (ex.: fidelidade marcada), um **alerta visivel**
     (ex.: "revisar fidelidade") -- o operador julga;
   - botoes **Aprovar** / **Reprovar**.
4. **Confirmacao**: operador confirma o conjunto aprovado.
5. **Fase pesada (baixar-video + cortar)**: so agora o sistema baixa o video e renderiza
   (corte + 9:16 + legenda + logo). Baixa o **video INTEIRO** com o downloader nativo
   paralelo e corta local — decidido POR MEDICAO (baixar so as janelas via
   `--download-sections` levou 577 s contra 7,3 s do video inteiro; o gargalo e paralelismo,
   nao volume). Ver "Fase pesada -- estrategia de download". Status: baixando-video,
   renderizando, concluido.
6. **Entrega**: a pagina lista os Shorts de finalizados/<id>/ para baixar. O operador
   envia por WhatsApp Web (fora do sistema).

## Escopo

Dentro:
- cmd/servidor (HTTP, porta configuravel, sem auth) + pagina unica servida via embed.
- Orquestracao em duas fases separadas por aprovacao humana:
  - fase leve: baixar-legenda -> Selecionar (harness);
  - (pausa: aprovacao do operador);
  - fase pesada: baixar-video (o video INTEIRO, downloader nativo paralelo) -> Renderizar
    (corta local os trechos aprovados, em tempo absoluto).
- Player YouTube embutido (IFrame Player API) por trecho, tocando de start a end.
- Maquina de estados do pedido cobrindo as duas fases + o estado de espera por aprovacao.
- Rotas: GET / (pagina), POST /pedidos (cria; dispara fase leve), GET /pedidos/{id}
  (status + candidatos quando prontos), POST /pedidos/{id}/aprovar (recebe a lista de
  trechos aprovados; dispara fase pesada), GET /finalizados/{id}/{arquivo} (baixar).
- Alinhamento de tempo: o start/end mostrado no player (YouTube) e usado no corte
  (arquivo baixado) devem referir-se ao MESMO instante do video. Garantir e testar.

Fora:
- Ajuste fino de corte pelo operador (marcar inicio/fim) -- era fora do escopo da v1;
  ENTROU na v2, ja implementada (secao no fim desta spec).
- Integracao de mensageria (WhatsApp) -- sempre manual.
- Retencao/limpeza de disco -- spec-06.

## Contratos

- POST /pedidos {youtube_url, inicio, fim} -> {id}, dispara a fase leve.
- GET /pedidos/{id} -> {id, status, erro, candidatos:[{indice, hook, start, end,
  duration_seconds, score, requer_revisao_reforcada, motivo_revisao}], shorts:[nomes]}.
- POST /pedidos/{id}/aprovar {aprovados:[indices]} -> dispara a fase pesada.
- Estados: baixando-legenda, selecionando, validando, aguardando-aprovacao,
  baixando-video, renderizando, concluido, erro (com mensagem clara).

## Ponto de atencao -- player YouTube vs arquivo baixado

O player do YouTube (IFrame API) pode "engasgar"/saltar para keyframe em seekTo (limitacao
conhecida do stream do YouTube). Na v1 (so assistir/aprovar) isso nao afeta o corte: o corte
usa o start/end ja calculado pelo harness sobre o arquivo baixado, nao o que o player
exibe. O player serve para REVISAO (o operador confere se o trecho presta), nao para definir
o corte.

**Na v2 o player TAMBEM define o corte, e isso deixou de exigir cuidado:** desde a troca para
baixar o video inteiro com origem 0, o tempo do player e o tempo do arquivo -- o mesmo
relogio, sem conversao. Ver a secao da v2 no fim desta spec.

## Tela de revisao -- layout (Parte 2, refinamento)

A primeira versao da tela de revisao era uma lista vertical com os 5 players carregados
de uma vez. Tres problemas: peso (5 iframes do YouTube nao renderizam -- ficam pretos);
nenhum estado visivel apos aprovar/reprovar; e o texto do trecho -- o artefato principal
para julgar doutrina -- ficava abaixo do player. As decisoes abaixo redesenham a tela.
Referencia visual/codigo: `docs/mockups/revisao-trechos_referencia.html`.

Decisoes (nao reabrir):

- **Um player so, reaproveitado.** Em vez de um iframe por candidato, um unico player do
  YouTube que faz `seekTo(start)` ao trocar de trecho. Resolve o peso; a pagina fica
  instantanea.
- **Um trecho em foco por vez, com trilha de status no topo.** Trilha compacta com N
  posicoes numeradas mostrando o estado de cada trecho: pendente (neutro), aprovado
  (verde/check), reprovado (vermelho/x), pede revisao (ambar/alerta). Posicao atual em
  destaque; clicar numa posicao pula para o trecho. Acima, o progresso ("Trecho 3 de 5 ·
  2 decididos").
- **Texto primeiro, player depois.** No card em foco: texto do trecho (fonte grande ~17px,
  o que se le para julgar doutrina) -> meta (start -> end · duracao · score) -> player ->
  controles. Ler e mais rapido que assistir; o video serve para julgar corte e entrega,
  nao o conteudo.
- **Auto-avanco.** Ao aprovar/reprovar, avanca para o proximo trecho pendente.
- **Navegacao por mouse e teclado.** Setas ‹/› flanqueando os botoes de decisao; trilha
  clicavel; atalhos: A aprova, R reprova, ←/→ navega, espaco toca/pausa.
- **"Ouvir a emenda do fim/inicio".** Botoes que tocam so a costura do corte: fim de
  `end-3s` a `end+2s`; inicio de `start-2s` a `start+3s`. Motivo: o corte as vezes termina
  no meio da frase (timestamp da legenda adianta o audio) -- em vez de assistir 46s, o
  operador ouve a emenda em ~5s.
- **Alerta doutrinario em duas camadas, com um tratamento por classe (quatro) em tres
  niveis de intensidade.** Ambar na trilha
  (o operador ve antes de chegar que o trecho pede atencao) e uma faixa no card com o
  `motivo_revisao` em texto. Nunca esconder trechos marcados. Mas para nao cair em fadiga
  de alerta (⚠ em tudo deixa de ser alerta), a INTENSIDADE segue a classe do confronto
  doutrinario (spec-14):
  - `desalinhamento` -> **alto**: ⚠ ambar, destaque forte, com o ponto citado da Declaracao.
  - `ambiguo_isolado` -> **medio, com ACAO**: ✂ "o corte ficou curto — o sermao esclarece;
    considere estender". NAO e alerta de doutrina: e convite a ajustar o trecho -- e o ajuste
    manual da v2 (ja implementado) e onde essa acao se realiza.
  - `provavel_erro_transcricao` -> **baixo**: ℹ neutro/quieto (cinza), "conferido: provavel
    erro de transcricao".
  - `fiel` (marcado pela Fase 4, confronto nao achou) -> **baixo**: ℹ neutro/quieto,
    "conferido: sem problema doutrinario aparente".
  - sem confronto ainda (spec-14 nao implementada) -> ⚠ ambar generico (comportamento atual).
  O operador ve TODOS os marcados, mas sabe onde olhar primeiro — e em dois casos sabe O QUE
  FAZER (estender o corte; conferir o texto). Enquanto a spec-14 nao existe, so ha o nivel
  generico; os niveis entram junto com ela.
- **Mostrar QUAL criterio afundou o score, em PALAVRAS — nunca a grade crua.** O card exibe
  um ou dois criterios mais fracos em linguagem natural, ex.: *"o que puxou o score:
  completude — o trecho nao fecha"*. NAO mostrar a planilha dos cinco numeros
  (`fidelidade 12/30`, etc.) ao operador: numero convida a **falsa precisao** — parece
  medicao quando e palpite de modelo, e o pastor pode tratar "12/30" como fato. O que e
  acionavel nao e o numero, e **o nome do problema**.
  - Exemplo real observado: um trecho tinha fidelidade baixa E **completude** baixa — o
    texto terminava pendurado ("Aí foi aí que aconteceu"). A completude e informacao NOVA,
    acionavel (o corte nao fecha) e **independente de doutrina** — o operador consegue
    decidir sozinho, sem julgar teologia.
  - A grade crua continua existindo para DESENVOLVEDOR (`cmd/auditar -criterios`), onde
    falsa precisao nao e risco: quem le sabe que e saida de modelo.
- **Rodape fixo** com a contagem ("1 aprovado · 1 reprovado · 3 pendentes") e o botao
  "Confirmar e gerar", sem precisar rolar ate o fim.
- **CSS na mao, sem Tailwind.** E uma tela so, poucos componentes; Tailwind exigiria build
  (contra a escolha de HTMX sem build) e o CDN e pesado. CSS com variaveis para cor e
  espacamento. HTMX cuida das requisicoes (criar pedido, confirmar aprovados); o JS vanilla
  cuida do player e da navegacao local (trocar de trecho nao vai ao servidor).
- **Identidade visual.** Fonte Google Sans Flex de `assets/fontes/` (servida pelo servidor
  em `/assets/`); acento verde-limao da igreja.
- **O texto falado vem do servidor.** O card mostra o texto REALMENTE falado na janela
  `[start, end]` (reconstruido da transcricao via `harness.Frasear`, o mesmo do
  `cmd/auditar`), nao o hook. Se faltar (edge case), cai para o hook.
- **Ordenacao: revisao cronologica, render por score (dois contextos, duas ordens, de
  proposito).** Na tela de revisao a ordem e CRONOLOGICA (ordem do sermao), NAO por score.
  Tres motivos: (a) a marcacao de fidelidade DERRUBA o score (spec-11), entao ordenar por
  score empurra os trechos marcados para o fim da fila — justo os que mais precisam do olho
  do pastor, o oposto do proposito; (b) o score erra de forma conhecida (no run do
  `IxmiQGL9CMQ`, a frase quebrada "como o que nos somos os filhos deles" levou score 88, o
  mais alto) — ordenar por sinal ruidoso ancora o operador no item errado; (c) trechos
  vizinhos compartilham o contexto do sermao, o que ajuda a julgar se o trecho se sustenta
  sozinho. Na saida do RENDER a ordem por score continua (`short_01` = melhor e util para
  publicar; ver `internal/video`). Para destacar o melhor na revisao SEM reordenar, um selo
  discreto ("★ maior score") no card. O indice ORIGINAL do candidato e preservado apos a
  reordenacao (o `/aprovar` usa esse indice).

## Temperatura padrao = 0 (auditabilidade) e "buscar outros trechos"

- **Default de temperatura do modelo = 0** (greedy), no harness e no servidor (era 0.2 do
  spike). O argumento decisivo e **AUDITABILIDADE**, nao so confiabilidade pastoral: durante
  o desenvolvimento, a variancia entre execucoes foi um confusor permanente em cada
  diagnostico — toda saida diferente exigia perguntar "e bug ou e o modelo?". Com temp 0,
  "mesma entrada, mesma saida" vira ferramenta de depuracao: **se mudou, alguem mexeu em
  algo**. Configuravel por `HARNESS_TEMP` (nao recompila).
- **A perda, registrada:** temp 0 congela um unico caminho greedy. Se o sermao tem oito
  trechos bons e o greedy acha quatro, os outros quatro **nunca aparecem, nem re-rodando**.
- **Sem promessa de reprodutibilidade exata:** e "reprodutivel na pratica". Medido: 3/3
  runs identicos no `IxmiQGL9CMQ`; mas no sermao grande `174206-3` deu 4 vs 3 a temp 0. O
  residuo e **nao-determinismo de hardware** (ordem de reducao em ponto flutuante na GPU +
  roteamento do MoE) — **NAO** o speculative decoding: medido, rodar temp 0 SEM o draft
  model deu 4/5/6 (pior, nao melhor). Ou seja, nao ha conserto barato para determinismo
  total; o draft fica (e mais rapido e, se algo, mais estavel). Nao prometer "identico
  sempre" — sermoes com muitos trechos-limite variam mais.
- **"Selecionar de novo" = "buscar outros trechos" (a implementar; so o desenho aqui).**
  Com o default 0, re-rodar devolveria o mesmo conjunto — o botao so faz sentido se re-rodar
  com **temperatura maior** (ex.: `HARNESS_TEMP=0.5`). Fica como acao EXPLICITA do operador,
  rotulada pelo que faz ("buscar outros trechos"), para quando ele achar que veio pouco. O
  padrao fica confiavel e reprodutivel; a exploracao passa a ser escolha consciente, nao o
  comportamento default. (Nao implementado ainda.)

## Fase pesada -- estrategia de download e alinhamento de tempo (Parte 3)

Ao aprovar, o servidor dispara em background: baixar o video dos aprovados -> renderizar ->
listar os finais. Decisoes:

- **IMPLEMENTADO: baixa o VIDEO INTEIRO com o downloader nativo paralelo**
  (`--concurrent-fragments 8`, sem `--download-sections`) e corta local no render. Substituiu
  a janela contigua por decisao de medicao (abaixo). Contrato: **origem 0** — o arquivo comeca
  no inicio do video, entao o render corta em tempo ABSOLUTO, sem calculo de origem a
  propagar. `download.BaixarVideoCompleto` **devolve** essa origem (era uma constante
  `origemVideoCompleto` no servidor; virou valor de retorno do escritor do arquivo, porque
  duas afirmacoes do mesmo fato divergem — ver spec-09).

- **MEDIDO (com runtime JS instalado): o fator dominante e PARALELISMO, nao assinatura nem
  travessia.** Todas as abordagens medidas no mesmo sermao (`IxmiQGL9CMQ`, 46 min, yt-dlp
  2026.07.04, **com deno**):

  | abordagem | bytes | parede | throughput |
  |---|---|---|---|
  | (A') contigua 18 min (`--download-sections`) | 98 MB | **577 s** | 174 KiB/s |
  | (B') `-g` + ffmpeg `-ss` ANTES do `-i`, `-c copy`, trecho 50 s | 2,4 MB | **29 s** (+2 s p/ resolver) | ~84 KiB/s |
  | (C) 4 secoes ~50 s (`--download-sections` x4, sem runtime) | 18 MB | 129 s | — |
  | **(D') video INTEIRO, `--concurrent-fragments 8` (nativo)** | 125 MB | **7,3 s** | **26+28 MiB/s** |

  **(D') e ~79x mais rapido que (A')** e ~4x mais rapido que (B') — baixando o video inteiro
  (46 min) em menos tempo do que (B') leva para pegar 50 segundos.

  **Qual fator dominava (a pergunta que os numeros de ontem levantaram):**
  - **NAO era a assinatura/runtime JS.** (A') com runtime = **577 s**, praticamente igual aos
    576 s medidos SEM runtime. Se o estrangulamento viesse de URL mal assinada, o runtime
    teria consertado; nao mudou nada.
  - **Nao e (so) travessia.** (B') usa range-request de verdade (transferiu so 2,4 MB, sem
    percorrer os 21 min ate o trecho) e AINDA assim levou 29 s — ~84 KiB/s.
  - **E PARALELISMO.** Todo caminho que usa o **ffmpeg como downloader** (A' e B') abre UMA
    conexao e e estrangulado a ~84-174 KiB/s. O downloader **nativo** do yt-dlp abre 8
    fragmentos em paralelo e atinge ~26 MiB/s — **~150x**. A travessia do #6273 explica os
    BYTES a mais de (A'), e a regressao #15036 piora o caminho seccionado, mas o que domina
    o TEMPO e conexao unica vs fragmentos paralelos.
  - (D) foi de 18 s (sem runtime) para 7,3 s (com) — pode ser o runtime dando URLs melhores,
    pode ser variacao de rede; uma amostra nao distingue, e a ordem de grandeza ja era a
    mesma. Nao atribuir ao runtime sem mais medicoes.

  **Consequencia para a arquitetura (DECIDIDO e IMPLEMENTADO):** baixar **o video inteiro
  com o downloader nativo paralelo** e o mais rapido E o contrato mais simples (origem =
  inicio do video, tempo absoluto, sem calculo de origem). Baixar por segmento (URLs
  efemeras do `-g`, mais requisicoes, mais risco de 429) **nao se paga em tempo** — o ganho
  seria de disco, e disco e problema da **spec-06 (retencao/limpeza)**, que passa a ser
  prioridade real: ~125 MB por pedido ficam em `trabalho/<id>/`.

  Riscos ja levantados do colapso download+render numa passada (se um dia for reconsiderado):
  URLs do `-g` sao temporarias e atadas ao IP (usar ja, re-resolver no retry, expiracao =
  erro claro); falha do render perde o download e acopla os estados baixando/renderizando
  (perde diagnostico); o ajuste fino da v2 precisa de material em disco; duas entradas (v+a)
  exigem `-map` e `-ss` em cada.

- **`-ss` do render: preciso E rapido — MEDIDO, nao ha deslizamento.** O render faz
  `-ss <start absoluto>` ANTES do `-i` sobre o video inteiro. Duas propriedades, ambas
  verificadas:
  - **Preciso.** Existe um mito de que `-ss` antes do `-i` "salta para o keyframe" e o corte
    comeca ate ~1 GOP antes. Isso vale para **`-c copy`** (sem recodificar). O render
    **RECODIFICA** (aplica crop/scale/legenda/logo): nesse caminho o ffmpeg faz o seek por
    keyframe e entao **decodifica ate o instante exato** antes de comecar a codificar. Prova
    medida: frame do `short_01` em t=0,5 s contra o frame do video original em 1318,5 s
    (= o start aprovado) deu **PSNR 44,78 dB** (praticamente identico; o residuo e o
    re-encode), enquanto a contraprova 32 s adiante deu 16,75 dB. **Nao houve deslizamento
    algum.** NAO "consertar" isto — o comportamento medido esta correto.
  - **Rapido.** O `-ss` antes do `-i` usa o indice do MP4 e salta: offset 5 s = 2,34 s,
    offset 1318 s = 2,55 s, offset 2400 s = 2,52 s (o offset nao custa nada). Se alguem
    mover o `-ss` para DEPOIS do `-i`, o ffmpeg decodifica tudo ate o ponto: 20,74 s / 183 s
    de CPU — 8x mais lento. Travado por `TestArgsFFmpegSeekAntesDoInput`.

- **Alinhamento de tempo (como ficou).** Servidor: video inteiro -> origem 0 (devolvida pelo
  baixador e gravada em `pedido.json`/`video.json`) -> o render corta em tempo ABSOLUTO. CLI (`cmd/baixar` + `cmd/render`): o video.mp4 e a
  janela `[ped.Inicio, ped.Fim]`, entao a origem e `ped.Inicio` (`video.Renderizar`, que
  chama `RenderizarComOrigem` com essa origem). As duas origens convivem porque o render
  recebe a origem EXPLICITA; testado em `TestRenderizarComOrigemAlinhaCorte` (o `-ss` do
  ffmpeg bate exato nos dois casos) e no fluxo do servidor (`TestFaseHeavyFluxoCompleto`
  exige origem 0).
## Limitacao de ORIGEM: a qualidade do Short e limitada pela transmissao, nao pelo pipeline

Dado do dono: as transmissoes da igreja sao no maximo **720p**. Isso poe um teto que NENHUM
ajuste de codigo remove:

| fonte | pixels reais do corte 9:16 (centro) | saida | ampliacao |
|---|---|---|---|
| **720p (hoje)** | **405 x 720** | 1080x1920 | ~2,7x em area |
| 1080p | 608 x 1080 | 1080x1920 | ~1,2x em area |

O Short e um corte 9:16 do CENTRO do quadro: de 1280x720 sobram so 405x720 pixels reais,
esticados para 1080x1920 — **mais da metade dos pixels do Short e interpolada**. E o que
explica a leve moleza percebida nos frames, principalmente no rosto.

- **Mitigacao no codigo (feita):** o escalador passou a ser `flags=lanczos` (o bicubico
  padrao e mais macio em ampliacao). Custo medido: **zero** — 3,03 s com lanczos contra
  3,05 s com bicubico, num Short de 40 s. Ganho visivel em traco fino (cabelo, sobrancelha,
  rugas); ver `frames-teste/comparar_lanczos.png`.
- **A melhoria de MAIOR impacto esta FORA do codigo: a igreja transmitir em 1080p.** Isso
  daria 2,25x mais pixels REAIS no corte — muito acima de qualquer ganho de filtro. Fica
  registrado aqui para nao se perder: quando houver conversa sobre equipamento/OBS da
  transmissao, esta e a alavanca.
- O seletor de formato ja esta preparado (`bv*[height<=1080]`, ver `download.FormatoPadrao`):
  hoje pega 720p porque e o que existe; no dia em que a transmissao subir, o pipeline
  aproveita sozinho, sem mudar codigo.

### Resolucao de saida: manter 1080x1920 (decidido, com medicao)

Avaliado emitir 720x1280 (mais proximo do nativo) e deixar a plataforma escalar. Medido no
mesmo Short de 40 s: **720x1280 = 1,66 s** contra **1080x1920 = 3,03 s** (~1,8x mais rapido,
arquivo 48% menor). Mesmo assim, **manter 1080x1920**:

1. **Legenda e logo NAO sao upscale — sao rasterizados na resolucao de saida.** O video ja
   perdeu (e upscale de 405px de qualquer forma), mas o TEXTO da legenda e a logo sao
   desenhados nativamente no frame final. Em 1080x1920 eles saem nitidos de verdade; em
   720x1280 seriam rasterizados menores e a plataforma os ampliaria — texto borrado, que e
   o elemento mais visivel do Short. Emitir 1080 preserva a nitidez do que ainda TEMOS em
   resolucao nativa.
2. **As plataformas favorecem largura >= 1080** (perfil de entrega/bitrate melhor).
3. **O custo nao importa no total:** 1,4 s por Short e ruido perto do download; o render
   inteiro ja e ~3 s.

## Nota historica: `--force-keyframes-at-cuts` (NAO se aplica a fase pesada atual)

**Nao vale mais para o servidor.** A flag so tem efeito quando o yt-dlp corta SECOES
(`--download-sections`) — ela forca keyframes nos pontos de corte. A fase pesada baixa o
**video inteiro**: nao ha secoes, nao ha cortes no download, a flag nao faria nada. Estado
do codigo (conferido): `argsVideoCompleto` (servidor) **nao passa** a flag; `argsVideo`
(caminho seccionado, usado pelo `cmd/baixar` no fluxo CLI) **passa** — e ali ela faz
sentido. Nada a remover.

**Registro para o caso de voltarmos a baixar por secao:** com a flag, o arquivo comeca
EXATAMENTE no tempo pedido (ao custo de recodificar), entao a origem e conhecida (= start
pedido). SEM a flag, o corte da secao cai no keyframe anterior e a **origem do arquivo
desliza** (por ate ~1 GOP), quebrando o contrato de tempo — nesse caso a origem teria que
ser MEDIDA do arquivo (ffprobe do 1o keyframe), nunca assumida.

(Nao confundir com o `-ss` do RENDER, que e outro assunto e esta medido acima: ali o
re-encode garante corte exato, sem deslizamento.)
- **Erro nunca trava.** Falha no download ou no render -> pedido vai para `erro` com
  mensagem clara na tela (nunca fica eternamente em baixando-video). Um erro visivel e melhor
  que um spinner infinito.
- **Entrega.** `GET /finalizados/{id}/{arquivo}` serve os Shorts (whitelist dos arquivos que
  o pedido gerou -- sem travessia de caminho). O operador baixa e envia por WhatsApp manual.
- **Progresso por polling (provisorio).** baixando-video -> renderizando -> concluido pelo
  mesmo `every 2s`. Migrar para SSE e ideia registrada (docs/ideias-futuras.md).

## Criterios de aceite

- [ ] Servidor sobe na porta configuravel (padrao :7799), sem auth; GET / serve a pagina.
- [ ] POST /pedidos valida entrada, cria o pedido e dispara a fase leve; responde id na
      hora (nao bloqueia).
- [ ] A fase leve baixa SO a legenda (nao o video) e roda a selecao; sem legenda -> erro.
- [ ] A pagina lista os candidatos, cada um com player YouTube embutido tocando de start a
      end, hook/dur/score, alerta de revisao quando marcado, e botoes aprovar/reprovar.
- [ ] POST /pedidos/{id}/aprovar dispara a fase pesada so para os aprovados.
- [ ] A fase pesada baixa o video INTEIRO (nativo paralelo) e renderiza; a pagina lista os
      Shorts finais para baixar.
- [x] O start/end do corte corresponde ao mesmo instante mostrado no player (v2: e o mesmo
      relogio, sem conversao -- ver abaixo).
- [ ] Erro em qualquer fase aparece na pagina com mensagem clara.
- [ ] Testes: rotas (httptest), maquina de estados das duas fases com download/selecao/
      render mockados; validacao de entrada. Nao subir pipeline real nos testes.
- [ ] go build ./... e go test ./... verdes.

## v2 -- ajuste manual do corte pelo operador (IMPLEMENTADO)

O operador detectava um corte ruim com "ouvir a emenda" mas so podia REPROVAR um trecho cujo
conteudo era bom -- desperdicio do trecho e do trabalho do modelo. A v2 fecha isso.

### O alerta de alinhamento de tempo CAIU

A v1 avisava que o tempo do player podia nao corresponder ao do arquivo baixado. **Nao
corresponde mais ao problema: os dois relogios sao o MESMO.** O download passou a ser o
video INTEIRO com origem 0, e o player do YouTube tambem conta do
inicio do video. Logo `player.getCurrentTime()` devolve o tempo absoluto que o corte vai
usar, **sem conversao nenhuma**. O alerta antigo vinha do `--download-sections`, que nao e
mais usado (ver a decisao da fase pesada, acima).

Sobrou apenas o cuidado com o `seekTo`, que pode saltar para keyframe -- irrelevante aqui,
porque o operador **marca** o tempo (getCurrentTime) em vez de depender da precisao do salto.

### Servidor

`POST /pedidos/{id}/ajustar` recebe `{indice, start, end}` (segundos) e devolve o trecho
recalculado: `hook`, `duration_seconds`, o **texto realmente falado** na janela nova (via
`harness.Frasear`, o mesmo do `cmd/auditar`), os tempos efetivos e `aprovavel`/`motivo`.
E leve de proposito -- nada de disco pesado nem de modelo -- porque o cliente o chama a cada
ajuste. **O texto novo e essencial:** e o que o operador esta julgando.

`POST /aprovar` aceita, por trecho aprovado, os tempos ajustados (JSON e formulario), e sao
esses que vao ao render (`candidatosAprovados` aplica o ajuste).

**O hook e recalculado sempre**, pela mesma regra da Fase 3 (`hook = primeira frase real a
partir do start final`): ao estender para tras, o hook deixa de ser a frase-ancora e passa a
ser a abertura de fato.

### Encaixe: assimetrico, e o porque

**As DUAS pontas sao livres para frente e encaixam so para tras**, com folga limitada a 5 s:

| Lado | Comportamento | Hook |
|---|---|---|
| Inicio | aceito se em ou depois do comeco de uma frase; antes, encaixa para FRENTE | a frase que **contem** o start |
| Fim | aceito se em ou depois de uma fronteira de frase completa; antes, encaixa para FRENTE | (nao afeta) |

A causa e uma so: **o carimbo da legenda adianta o audio em 1-3 s.** Encaixar na fronteira mais
proxima usa esses mesmos carimbos errados e devolve o operador a fronteira defeituosa. Andar
para frente nunca corta fala; para tras, sempre pode.

**Correcao de rota registrada.** A primeira versao tratava o inicio como exato ("o inicio deve
ser exato" foi a suposicao da epoca) e encaixava na fronteira mais proxima. Isso deixou o
operador **sem saida** na ponta do inicio: com o corte em `00:20:08` ele ainda ouvia
`"...do pelo Senhor"`, clicava "mais tarde", ia para `00:20:09` e o sistema o devolvia para
`00:20:08` -- o botao nao fazia nada visivel. Era o mesmo argumento que ja tinha liberado o
fim; o defeito da fonte nao distingue as pontas.

**A diferenca crucial no inicio:** o hook e a frase que **contem** o start, nao a seguinte. Com
start em `00:20:10`, o hook segue `"Todo cristao deve estar preparado…"` -- que e o que se ouve.
Na faixa de frases essa frase continua destacada como dentro do corte, e o texto falado comeca
nela (por isso o texto e o destaque partem da FRONTEIRA da frase do hook, e nao do start
efetivo: com o start adiante do carimbo, usar o start deixaria a propria frase do hook de fora).

A spec-16 foi reformulada nas duas pontas para enunciar a intencao real -- nao comecar no meio
de uma fala, nao terminar cortando uma. Nao e excecao aberta para o ajuste manual: a
delimitacao automatica (Fase 3) continua produzindo fronteiras exatas e passando sem folga.

Granularidade dos controles: **±1s no inicio; ±0,25s e ±1s no fim.** Frame a frame (±0,033s)
foi descartado -- o operador julga de ouvido, e fronteira de fala e evento de 0,1-0,3 s.

> Nota: agora que o inicio tambem e livre, o passo fino faria sentido ali tambem. Nao foi
> adicionado porque o operador acabou de testar esta tela e a assimetria atual ja esta
> compreendida; mexer nos controles de novo sem pedido custaria mais do que rende. Fica
> registrado como ajuste possivel se ele sentir falta.

### Faixa de duracao: uma fonte so

A divergencia entre "30-58 s" (Fase 3) e "30-60 s" (Fase 5 e auditor) nao era bug, era
**margem sem nome**: 58 s e a regua de CONSTRUCAO (2 s de folga para o teto de 60 do Short e
para a margem de fim da spec-10); 60 s e o PORTAO de validacao. Um portao mais estreito que a
regua transformaria arredondamento de 0,1 s em descarte. Agora tem nome e um lugar so --
`harness.DuracaoMinMs`/`DuracaoMaxMs` --, usado pela Fase 3 **e** pelo ajuste manual.

### Guardas -- no servidor, nao no cliente

O servidor RECALCULA em vez de confiar no que o cliente mandou: um POST direto (ou um JS
desatualizado) nao pode enfiar um corte de 64 s no render. Recusa com mensagem que traz os
numeros ("ficaria 64.0s, o maximo e 58s -- encurte 6.0s"), impede `end <= start` e clampa nos
limites da pregacao informada. O cliente repete a regra apenas para nao frustrar o operador
no ultimo clique (botoes "Aprovar" e "Confirmar e gerar" desabilitados com o motivo no
`title`).

### Cliente -- REDESENHADO apos teste com o operador

A primeira versao funcionava e o operador nao entendeu. O diagnostico do dono, com a resposta
de cada ponto (mockup em `docs/mockups/ajuste-corte_referencia.html`):

| O que confundia | Correcao |
|---|---|
| "Marcar aqui" nao dizia **aqui onde** -- clique as cegas | botao passa a nomear o tempo: **"usar 00:39:24 do player"**, com relogio ao vivo |
| "−1s"/"+1s" nao diziam o que fazem: "−1s" no inicio deixa o trecho MAIS longo, "+1s" no fim tambem -- o mesmo rotulo com efeitos opostos por linha | rotulo pelo **efeito**: "‹ mais cedo" / "mais tarde ›" |
| o numero que muda ficava numa linha embaixo, longe dos botoes que o mudam | o valor fica **entre** as duas setas |
| a assimetria (Fim com 5 botoes, Inicio com 3) parecia defeito | passo fino rebaixado a **linha subordinada, rotulada "ajuste fino", so no Fim** -- le como recurso extra, nao como falta |
| "54.75s" e "00:40:12.000" exibiam precisao que o sistema nao tem | segundos inteiros e `HH:MM:SS` na tela; o `.000` fica so no contrato interno do Candidato |
| "ouvir o fim" longe dos controles do fim | movido para a linha do Fim, destacado |
| resumo com dois timestamps | **linguagem natural**: "1s a menos que o original · fica com 54s" |

**A mudanca principal: faixa de frases clicavel.** Acima dos controles, as frases em volta do
trecho (as de dentro destacadas). Clicar move o inicio (se a frase estiver antes do meio do
trecho) ou o fim (se depois). Troca **"empurrar ate acertar" por "apontar onde e"**.

E nao e recurso novo: o servidor **ja** encaixa o corte em fronteira de fala usando o
`Frasear`, entao a frase e a unidade nativa do ajuste -- a faixa expoe o que existia
escondido. O servidor passou a devolver a vizinhanca (`vizinhanca[]` com `inicio_ms`,
`fim_ms`, `rotulo`, `texto`, `dentro`) junto do recalculo, do mesmo `Frasear`: uma fonte so,
o cliente nao refraseia nada. A faixa vem **inclusive quando o ajuste e invalido** -- e
justamente quando o operador precisa dela para se orientar.

Dois detalhes que decidem se funciona na pratica, mantidos do desenho anterior: (a) `efetivo(i)`
e a fonte unica da tela, entao player, "ouvir a emenda" e texto usam os tempos ajustados --
senao o operador ajusta e continua ouvindo o corte antigo; (b) o cliente adota os tempos que o
servidor DE FATO usara (apos encaixe/clamp), senao o empurrao seguinte partiria de um numero
que nao existe mais. Mantidos tambem: bloqueio de aprovar com ajuste invalido, restaurar
original, meia velocidade, atalhos e o debounce de 350 ms.

### Licao do primeiro teste no navegador: string presente != referencia integra

O redesenho subiu com um bug que nenhum teste pegou. Um recorte de bloco durante a edicao
engoliu `playPause`, `doInicio`, `emendaInicio` e `emendaFim`; a substituicao seguinte que
deveria atualizar essas funcoes falhou **em silencio** (nao encontrou o texto e nao reclamou).
Resultado: `node --check` passou (sintaxe valida), os testes de contrato passaram (as strings
que eles procuravam continuavam la) e a tela quebrou no primeiro clique com
`playPause is not defined`.

Duas correcoes, alem de restaurar as funcoes:

**1. Testes de INTEGRIDADE DE REFERENCIA** (`js_referencias_test.go`), sem navegador nem node:

- todo handler (`addEventListener('click', f)` e o atalho `ligar('id', f)`) aponta para funcao
  declarada no script;
- nenhuma chamada a nome nao declarado nem constante numa lista explicita de globais
  permitidos (APIs do navegador, `YT`, `htmx`) -- lista explicita de proposito: nome novo ali e
  decisao consciente;
- todo id que o JS busca existe em algum template (o outro lado da mesma falha: id renomeado
  no HTML e nao no JS da `null`);
- uma lista nomeada das funcoes essenciais da tela, redundante de proposito: se um recorte
  remover a funcao E o handler juntos, os testes de referencia ficam satisfeitos e a
  funcionalidade desaparece calada.

**2. `ligar(id, fn)` no lugar de `getElementById(...).addEventListener(...)` direto.** O que
transformou uma funcao ausente em tela totalmente morta foi a exceção no PRIMEIRO
`addEventListener` abortar o `ligarControles` inteiro. O sintoma ("nada funciona") nao apontava
para a causa (uma funcao). Agora um controle quebrado degrada so a si mesmo e o console nomeia
qual.

A releitura do JS que essa investigacao forcou achou um segundo bug, este nunca observado: sem
ajuste, `efetivo()` nao consultava o cache de vizinhanca (`REV.viz`), entao navegar para outro
trecho e voltar deixava a faixa de frases travada em "Carregando…" -- `garantirVizinhanca` ja
tinha respondido e nao pediria de novo.

### Segunda rodada com o operador: 8 a 10 escutas por trecho

A faixa de frases funcionou, mas ajustar um trecho custava 8 a 10 escutas. Seis correções.

**1. As reproducoes discordavam entre si (bug).** O relato foi "com 'ouvir o fim' o corte esta
perfeito, mas tocando do inicio o trecho termina antes". A causa NAO era o `efetivo()` faltar no
play — ele ja estava la. Era que **"ouvir o fim" tocava ate (fim + 2 s)** e "ouvir a emenda do
inicio" desde **(inicio − 2 s)**: as emendas mostravam audio que o Short **nao vai conter**. O
operador ajustava ate a emenda soar bem, tocava do inicio (que para no `fim` real) e concluia
que o ajuste nao pegou.

Agora **toda escuta e do PRODUTO**: "ouvir o inicio" toca do start para frente, "ouvir o fim"
para no end. O limite de parada passou a ser reavaliado a cada tick (funcao, nao valor
capturado no clique), entao ajustar durante a reproducao move o ponto de parada junto.

> Consequencia retroativa que vale registrar: o diagnostico da rodada anterior ("com o corte em
> 00:20:08 ainda ouco '...do pelo Senhor'") pode ter vindo da emenda antiga, que tocava 2 s
> ANTES do corte. O adiantamento da legenda segue valendo — foi confirmado por outras vias —
> mas a ferramenta de escuta contaminava o diagnostico, e isso explica parte das 8 a 10
> escutas: duas ferramentas discordando sobre o mesmo corte.

**2. Botoes duplicados** (pedido que se perdeu entre rodadas): havia dois "usar <tempo> do
player". O do Inicio saiu — com a faixa de frases, clicar na frase e melhor e mais preciso — e o
do Fim entrou na linha do Fim, com rotulo curto ("usar 00:05:13") para caber.

**3. Escuta automatica apos ajustar** — a correcao que derruba o custo. Conferir tocando o trecho
inteiro custa ~49 s por iteracao; dez tentativas sao oito minutos. Agora, quando o debounce
assenta, toca sozinho os ~5 s da ponta que foi mexida. **O ciclo cai de ~50 s para ~5 s**, e
"tocar do inicio" volta a ser conferencia final unica em vez de ferramenta de trabalho.

**4. Layout em duas colunas** (video de um lado, texto e controles do outro) e **remocao do
paragrafo grande do topo**: a faixa de frases mostra o mesmo texto e melhor, porque destaca o
que esta dentro do corte. Manter os dois empurrava os controles para fora da tela. Abaixo de
900 px volta a empilhar. O painel deixou de ser `<details>`: com duas colunas nao ha motivo para
esconder.

**5. Escutas consolidadas:** uma de cada, junto do controle a que pertence.

**6. Frases com o MESMO carimbo: agrupadas numa entrada so.** A legenda tem resolucao de 1 s,
entao duas frases no mesmo segundo recebem carimbos identicos (no print, "Isso e um dos seus
maiores alegados." e "Os puritanos acreditavam…" ambas em 00:04:21). Na faixa isso produzia duas
linhas distintas levando ao MESMO tempo: o operador clicava na segunda, nada mudava, e somava
mais um "nao funcionou".

Escolhi **agrupar** em vez de desempatar pela ordem. Desempatar exigiria atribuir a segunda
frase um tempo que **ninguem mediu** — interpolado entre carimbos — e cortar ali poria o inicio
do Short num ponto arbitrario no meio da fala, com aparencia de precisao que o dado nao tem: a
mesma falsa precisao ja rejeitada na duracao e nos rotulos. Agrupar e honesto: para efeito de
corte aquelas falas sao um bloco indivisivel, e a faixa passa a dizer isso ("2 falas no mesmo
segundo"). A perda e real e aceitavel — ele nao pode comecar na segunda frase do bloco, mas nao
podia antes tampouco; antes a interface **fingia** que podia.

### Terceira rodada: tela simplificada (mockup `revisao-simplificada_referencia.html`)

O layout lado a lado da rodada anterior criou um desperdicio: a coluna esquerda quase vazia (o
video ocupava um terco, o resto era espaco morto) enquanto os controles se espremiam na direita e
quebravam em duas linhas. Tres mudancas.

**Fluxo de DOIS CLIQUES, no lugar da heuristica do meio.** O sistema adivinhava qual ponta mover
comparando o clique com o meio do trecho — e quando errava, o operador nao tinha como entender
por que. Agora e deterministico: **1º clique define o inicio, 2º define o fim, 3º recomeca**, com
a instrucao visivel no topo da faixa (`1. clique onde COMECA` / `2. agora clique onde TERMINA`).
Ele sabe o que vai acontecer **antes** de clicar, em vez de descobrir depois.

O estado e por trecho, nao global: trocar de trecho nao herda o passo do anterior. "Restaurar"
recomeca o ciclo. Ao clicar no fim, o corte termina onde a **proxima fala comeca** — e isso que o
operador quer dizer ao apontar a ultima frase que deve entrar. Se o inicio escolhido passar do
fim atual, o fim e empurrado para a frase seguinte em vez de o operador levar um erro por um
clique legitimo.

**Setas em volta do valor:** `« ‹ 00:04:13 › »`. Esquerda = mais cedo, direita = mais tarde,
dupla = 1 s, simples = 0,25 s, com legenda pequena embaixo. Ocupa **um quinto** do espaco dos
botoes rotulados e a direcao e auto-evidente — nem "±1s" (que exigia traduzir o sinal em efeito)
nem "mais cedo/mais tarde" escrito (que ocupava a linha inteira). O texto migrou para o `title`
de cada seta, onde serve de dica sem gastar espaco.

Sairam: o "usar o tempo do player" (a faixa de frases faz melhor e mais preciso) e os botoes
separados de "ouvir inicio/fim" (a escuta automatica cobre).

**Layout invertido em relacao a rodada anterior:** controles a **esquerda, sob o video**, onde
havia o espaco morto e onde eles cabem sem quebrar; faixa de frases a **direita, com rolagem
propria** (`max-height` + `overflow-y`). A faixa tem uma duzia de frases e, sem rolagem interna,
empurrava o painel para fora da tela — que era o problema original. A faixa **rola sozinha ate as
frases selecionadas** ao trocar de trecho, com `scrollTop` e nao `scrollIntoView`: este ultimo
rolaria a pagina inteira e desfaria o ganho de caber numa tela.

Um selo `tocando a emenda…` avisa quando o som veio do sistema — a emenda toca sozinha, e sem
aviso o operador nao sabe se foi ele que clicou em algo por acidente.

### v4, fatia 3 (2026-07-30): duas réguas, e os marcadores arrastáveis

**Arranjo (marcação do dono).** Vídeo e faixa de frases lado a lado em cima; as duas réguas na
**largura inteira** embaixo; a linha de decisão por último.

A régua não cabe na coluna do vídeo, e é aritmética: a coluna tem ~450 px, e ±60 s ali dariam
**0,27 s/px** — um pixel por passo de 0,25 s, inutilizável. Com ~1120 px os mesmos ±60 s dão
**~0,11 s/px**, dois pixels por passo fino.

| régua | o que mostra | para quê |
|---|---|---|
| **geral** | a JANELA DA PREGAÇÃO, com o trecho marcado | contexto e salto (clique posiciona o player) |
| **ampliada** | centro do trecho ±60 s | é onde os marcadores moram e onde se ajusta |

A geral cobre a pregação, não o culto: fora dela estão louvor e avisos, e desenhar 1h50 para 33 min
de pregação desperdiçaria 70% da régua. Sem `fim` informado, cai para a duração do vídeo.

**As pausas são desenhadas** na régua ampliada, com altura pela duração (300 ms → 10 px; ≥1,5 s →
34 px). É onde o resultado da fatia 1 vira utilidade: a fronteira do corte é a pausa da fala, e sem
VER onde ela está o operador arrasta às cegas. Altura como pista de ordenação, nunca classificação —
a distribuição medida é unimodal (ver `docs/medicoes/pausas-de-fala.md`).

**Ímã no arraste, e ele mora no SERVIDOR** (`RaioImaMs = 200`). O cliente só posiciona; ao soltar,
pede o recálculo com `gesto: "arraste-inicio"|"arraste-fim"` e o servidor arredonda para a pausa se
houver uma a menos de 200 ms — na régua isso é menos de 2 px, imperceptível como salto e o bastante
para dispensar precisão de pixel. Longe de pausa **o pulso manda**: arrastar para o meio de uma fala
é escolha legítima (às vezes o trecho tem de cortar ali para caber na faixa).

Regra no servidor porque é a mesma família do encaixe do clique, e duas cópias da mesma decisão
divergem. Consequência boa: dá para verificar por mutação, o que uma regra no JS não daria.

**As três regras, agora completas e separadas:**

| gesto | regra | distância |
|---|---|---|
| clique em frase | próxima **pausa** | sem limite (pode ser +4,5 s) |
| arraste na régua | **ímã** para a pausa | ≤ 200 ms, senão vale o pulso |
| empurrão ±0,25/±1 s | vale o **pedido** | não encaixa nunca |

**Travas visíveis.** A faixa de duração válida (30–58 s a partir do início atual) é desenhada na
régua, e o arraste para de andar ao encostar nela — em vez de andar e ser recusado na aprovação. Os
números vêm do servidor (`harness.DuracaoMinMs/MaxMs` injetados no template): uma cópia deles no JS
divergiria na primeira mudança de um lado.

**O histórico separa pulso de encaixe.** Cada ação registra a `regra` da ponta que se moveu
(`pausa` | `ima` | `pedido` | `legenda`) ao lado de `pedido_ms` e `aplicado_ms`. Com as três colunas
na mesma linha, "o operador arrastou até ali ou o sistema arredondou?" se responde sozinha — era o
que o log não distinguia.

**O que NÃO saiu:** faixa de frases (navega por conteúdo; a régua navega por tempo), empurrões de
±0,25 s e ±1 s (agora ao lado da leitura de cada marcador), histórico de ações, campo "o que você
ouviu", restaurar. **A régua acrescenta, não substitui.**

**Simplicidade:** sem canvas — as pausas e os marcadores são `div`s posicionadas em %, e na janela
desenhada há ~34 pausas, não as 1835 do culto. Duas conversões (`pxParaMs`, `msParaPct`) e um
`setPointerCapture`. A régua geral não tem arraste: clique salta, e todo o gesto fino mora na
ampliada.

### v4, fatia 2 (2026-07-30): o preview usa o MESMO arquivo que o corte

**O que muda.** A tela de revisão troca o iframe do YouTube por um `<video>` apontando para
`GET /video/{videoID}`, servindo `videos/<id>/video.mp4` com `http.ServeFile`. A IFrame API sai
inteira — `YT.Player`, `seekTo`, `playVideo`, `getCurrentTime`, `setPlaybackRate` e o script
remoto.

**Por que, com número.** O operador escolhia o ponto ouvindo o YouTube e o corte acontecia no
arquivo baixado: duas fontes, dois relógios. A documentação da IFrame API declara que o `seekTo`
vai para o keyframe mais próximo "a menos que a porção já esteja bufferizada" — não-determinismo
por projeto. E a parada por polling ultrapassava o ponto: **+89 ms** de mediana (10 tentativas,
medido em headless), sempre positivo, o que fazia o operador ouvir mais do que o Short teria.
Servindo o mesmo arquivo, a discrepância **desaparece por construção**, não por compensação.

**Range requests vêm de graça.** Verificado no arquivo real de 902 MB: `Accept-Ranges: bytes`,
`206 Partial Content`, e um pedido de 100 bytes transferiu 100 bytes. É o que permite dar seek em
01:30:00 sem baixar o culto.

**A parada da escuta** passou de poll de 40 ms para `requestAnimationFrame` (~16 ms) sobre um
`currentTime` exato, sem folga de compensação: não há mais buffer nem API remota no meio.

**A medição do overshoot foi DESCARTADA, e não é buraco.** Ela existia para diagnosticar a parada
por polling contra um relógio REMOTO — o mecanismo que produzia +89 ms e que deixou de existir.
Sobrou um limite que é o piso do navegador (um quadro, ~16 ms, mais a latência de um `pause()`
local), abaixo de qualquer sílaba e sem decisão pendurada nele. Medir isso pediria abrir uma página
no navegador do dono para confirmar o inevitável. Registrado aqui para ninguém procurar a
pendência: ela não existe.

**O download do vídeo passou para a fase leve, em PARALELO com a seleção.** É a consequência
inevitável: player local exige arquivo local. Rede e GPU não competem — download de 63 s de média
fica escondido atrás de 29 s de seleção, e em 31 de 46 pedidos medidos o vídeo já está no cache.
A justificativa do fluxo invertido está registrada como superada para o vídeo (seção acima).

**Degradação honesta:** sem o arquivo (download falhou, sem espaço, sem baixador), a revisão
continua — texto, faixa de frases e ajuste por número — e a tela DIZ que não há escuta, em vez de
mostrar um player quebrado. `dadosRevisao.videoLocal` carrega esse fato.

**Um furo latente fechado no caminho:** o download escreve direto em `videos/<id>/`, e um yt-dlp
morto por prazo deixava um `video.mp4` parcial ali. Passando de 20 MB, `TemVideo` diria "tenho
vídeo" e o cache serviria um arquivo truncado — com `video.json` de uma tentativa anterior, o corte
sairia do arquivo errado. Agora a falha de download limpa o resíduo do cache, e só quando **não**
há registro (com registro, o vídeo é de um download que funcionou e não pode ser apagado por uma
falha posterior).

### v4, fatia 1 (2026-07-30): a fronteira do corte vem do ÁUDIO, não da legenda

**O bug, com número.** O clique em frase terminava o corte no início do bloco de legenda seguinte
(`fimDaFraseSeguinte`). Blocos da legenda *rolling* quebram por largura de tela, não por fim de
fala — então o corte caía no meio da frase falada, e às vezes no meio da palavra. Medido no culto
`fZGyLBofmmo`: fim em 01:30:06 contra fala corrida até 01:30:09.486, com **todos os deltas do
histórico de ações em zero** (o sistema aplicou fielmente um ponto que a legenda inventou).

**A correção.** Uma passada de `silencedetect` por culto (6,5 s medidos, guardada em
`videos/<id>/pausas.json`) dá as fronteiras REAIS. O clique em frase passa a terminar na primeira
pausa em ou depois do ponto pedido. No caso medido: 5405000 → **5409486**, deslocamento +4486 ms,
e o arquivo passa a terminar onde a fala para.

**Duas regras, e não se unificam** (`ContextoAjuste.Gesto`):

| gesto | regra | por quê |
|---|---|---|
| clique em frase | vai para a próxima **pausa**, sem limite de distância | é o conserto; limitar a distância recriaria o bug ("parou no meio porque estava longe") |
| empurrão ±0,25 s / ±1 s | **não encaixa**, vale o pedido | se encaixasse, cada clique voltaria à mesma pausa e o ajuste fino deixaria de existir |
| arraste (fatia 3) | ímã só se houver pausa em ~200 ms | pulso do operador manda; o ímã só arredonda |

**Nada em silêncio.** A resposta do `/ajustar` passa a dizer `regra_fim` (`pausa` | `legenda` |
`pedido`) e `deslocamento_fim_ms`; a tela mostra a frase "o fim foi levado para a pausa da fala em
X — +4,49 s do ponto da legenda" quando o deslocamento passa de 1 s. Se o encaixe jogar a duração
fora de 30–58 s, o trecho fica **não aprovável com o motivo** (guarda que já existia), em vez de
recusar sem explicar.

**Degradação honesta:** sem análise em disco (culto ainda não baixado), o encaixe cai na fronteira
da legenda e **declara** `regra_fim: legenda`. Quando o download passar para antes da revisão
(fatia 5), esse ramo deixa de acontecer na prática.

Parâmetros, distribuição e linha de base: `docs/medicoes/pausas-de-fala.md`.

### Histórico de ações do ajuste (2026-07-30) — instrumento, não enfeite

Depois de fechadas as camadas 1 e 2 (adiante), sobrou o relato mais difícil: **o áudio do Short
não corresponde ao trecho selecionado — às vezes passa, às vezes falta.** Duas causas possíveis,
com soluções OPOSTAS:

| se… | então o culpado é | e a saída é |
|---|---|---|
| o sistema aplicou **fielmente** o que o operador escolheu, e o áudio ainda não bate | o carimbo da legenda | alinhamento forçado (Rota D) — não tem outra |
| o **aplicado divergiu** do escolhido | nosso encaixe ou clamp | bug corrigível hoje |

Sem registrar o **par pedido/aplicado**, as duas são indistinguíveis e a discussão vira opinião.
Daí o histórico: uma linha por ação, com o que o operador pediu, o que o sistema aplicou depois
de encaixe e clamp, o delta, e a duração resultante.

**Onde nasce:** no `moverPara` do cliente — o funil por onde passam clique em frase, empurrão
fino, empurrão de 1 s e restaurar. Registrar em cada botão seriam quatro lugares para esquecer
um. O par se fecha quando a resposta do `/ajustar` chega (`completarAcao`).

**O terceiro dado — o que o operador OUVIU.** `pedido` e `aplicado` provam fidelidade do sistema;
nenhum dos dois mede o desvio REAL entre carimbo e áudio. Esse número só existe no ouvido do
operador, e é ele que decide entre um deslocamento fixo na Fase 3 e a Rota D. Por isso a ação
`ouvido` guarda uma frase livre ("faltou ~1s no fim"), opcional e de uma linha — se der atrito,
ninguém preenche, e campo vazio não mede nada.

**Ação substituída fica em branco, de propósito.** O debounce de 350 ms junta rajadas de
empurrões: quando a resposta chega, só a última ação corresponde ao estado respondido. As
anteriores ficam sem `aplicado` — inventar um valor ali faria o log afirmar o que não sabe.

**Persistência:** `resultados/acoes.csv`, ao lado do `cortes.csv`. Arquivo NOVO porque a unidade
é outra — o cortes.csv tem uma linha por trecho aprovado, o histórico tem N linhas por trecho.
Misturar mudaria o significado de cada linha e quebraria todo leitor atual (inclusive o cabeçalho,
que já quebrou duas vezes neste projeto). Colunas com **fonte única** (nome e valor na mesma
entrada), como no `tempos.csv`.

A decisão do trecho (`Aprovar`/`Reprovar`) entra como uma ação — fecha a sequência e mostra ONDE,
na ordem dos ajustes, o operador se deu por satisfeito. A gravação acontece no `/aprovar` (um POST
só, o que já existia); até lá o botão **copiar** leva a evidência em TSV a qualquer momento.
Trecho reprovado também é registrado, com `decisao=reprovado`: um trecho que ele mexeu e desistiu
é evidência igual.

Exemplo real (culto fZGyLBofmmo, trecho 3):

```
seq  tipo              pedido_ms  aplicado_ms  delta_ms  duracao_s  ouvido
1    frase-inicio      5361000    5361000      0         48.00
2    fino-fim          5409250    (vazio)      (vazio)   48.25      <- substituída pelo debounce
3    passo-fim         5415250    5415250      0         54.25
4    ouvido                                              54.25      faltou um pedacinho no fim
5    fino-fim          5415500    5415500      0         54.50
6    decisao-aprovado                                    54.50
```

Lido de uma vez: **delta 0 em toda ação** — o sistema aplicou exatamente o que foi pedido. Se o
áudio ainda não bater neste trecho, a causa não é nossa, e a Rota D deixa de ser hipótese.

### 2026-07-30: por que o ajuste fino "não pegava igual eu escuto" — TRÊS camadas

O operador relatou que, com ajuste fino, o corte não caía onde ele ouvia. Havia **três**
suspeitos, errando em direções diferentes; a investigação mediu cada um em separado, com
`docs/medicoes/medir_bordas.py` (fronteiras de fala por `silencedetect` na FONTE, que é a única
referência que não depende do carimbo da legenda).

| camada | o que fazia | medido | veredito |
|---|---|---|---|
| 1. perda no caminho | `hms()` = `MsParaHms(ms) + ".000"` — truncava o ms e afirmava zero | pediu 01:07:08**.250**, arquivo com 31,000 s; pediu 01:30:15**.250**, arquivo com 54,000 s | **culpado** (até 999 ms) |
| 2. escuta vs produto | audição parava no primeiro tique de 200 ms >= o fim, ouvindo além do corte | ponto escolhido pelo dono 107 ms antes do fim real da fala, e soou completo | **culpado** (até ~200 ms) |
| 3. corte do ffmpeg | `-ss`/`-t` com o tempo do candidato | áudio do arquivo comparado com a fonte em blocos de 20 ms: diferença média **0,1 dB** | **inocente** (exato ao ms) |

O que fecha a camada 3 merece registro porque poupa a próxima investigação: o corte do ffmpeg é
**sample-accurate**. O primeiro bloco de 20 ms do arquivo casa com o bloco correspondente da
fonte, inclusive os 28 ms de silêncio antes da fala entrar (−69,2 dB contra −69,8 dB).

**Consertos.** Camada 1: `hms()` passa a emitir o milissegundo real; dois testes, um por camada
(servidor → candidato, candidato → argumentos do ffmpeg), verificados por mutação. Camada 2: o
tique da audição cai para 40 ms e a parada acontece 40 ms **antes** do limite — assimetria de
propósito, mesma lógica do `encaixarFim`: a escuta nunca promete áudio que o arquivo não terá.

**Depois:** o mesmo ajuste do dono, medido de ponta a ponta, pediu 54,250 s e entregou 54,267 s
(+17 ms de quantização de quadro a 30 fps, para MAIS — o lado seguro). Com um empurrão de 0,25 s
além do ponto dele, o corte termina **143 ms depois** do fim da fala: palavra inteira.

**Por que nenhum teste pegava isso:** o `TestFluxoCompletoDoAjuste` comparava a tela com o
render, e os dois liam o mesmo formatador quebrado. Lição registrada no CLAUDE.md — consistência
interna não é correção.

### Retomada (`-retomar <id>`): iterar sem refazer o ciclo

Iterar em render ou em tela custava o ciclo inteiro por tentativa: ~40 s de selecao mais ~86 s
de download para olhar 3 s de render. Duas mudancas:

**O servidor grava `pedido.json`.** Sem isso, `cmd/render`, `cmd/auditar` e `cmd/limpar` NAO
funcionavam sobre pedidos criados pelo servidor — carregavam o arquivo e falhavam com "no such
file". Era uma lacuna, nao uma decisao.

**`cmd/servidor -retomar <id>`** poe um pedido que ja existe em disco direto em "aguardando
aprovacao", com os candidatos validados e o texto falado reconstruido da transcricao (nao de
cache, para nao divergir do que um pedido novo mostraria). As metricas comecam zeradas: o CSV
mede o ciclo DESTA execucao, nao a soma com a original.

**O video.mp4 e reaproveitado** quando ja esta em disco — cumprindo uma promessa que a spec-06
ja fazia por escrito ("o pedido retido pode ser regerado sem baixar de novo") mas que o codigo
nao aproveitava. `videoUsavel` exige tamanho minimo de 20 MB: um `.part` renomeado ou download
morto na metade passaria por "existe" e faria o render falhar com erro de ffmpeg, longe da causa.
O CSV de tempos ganhou a coluna `video_reusado`, senao um pedido reaproveitado entraria na media
de download como se tivesse baixado em 0 s.

**Por que e flag explicita, e nao carregamento automatico.** A spec-06 depende de o servidor NAO
carregar estado do disco: e isso que faz um pedido travado por crash desaparecer no restart e o
material bruto voltar a ser limpavel. Carregar tudo automaticamente transformaria pedido travado
em vazamento permanente. Com a flag, quem retoma e uma pessoa nomeando o pedido; o mapa continua
nascendo vazio e `TestReinicioLiberaPedidoOrfao` continua verdadeiro.

Pedidos anteriores a essa mudanca nao tem `pedido.json`; a retomada reconstroi o minimo do
`legenda.info.json`. O que NAO se recupera e a janela da pregacao (nunca foi persistida), e o
`Inicio` reconstruido vira `00:00:00` — porque o `cmd/render` usa `ped.Inicio` como a ORIGEM do
arquivo, e o video em disco e o inteiro. A degradacao e anunciada por aviso, nao silenciosa.

### Encode: medido, e a hipotese estava parcialmente errada

Nitidez medida pela energia de altas frequencias (laplaciano) no mesmo trecho, com a cadeia de
filtros de producao:

| saida | quadro | faixa da legenda | tempo/Short |
|---|---|---|---|
| veryfast crf20 (era o default) | 1.860 | 4.960 | 3,08 s |
| veryfast crf18 | 1.887 | 4.980 | 3,25 s |
| **medium crf20 (escolhido em 2026-07-29; ver abaixo)** | **1.921** | **5.031** | **5,29 s** |
| medium crf18 (escolhido em 2026-07-28) | 1.931 | 5.038 | 5,25 s |
| slow crf18 | 1.924 | 5.035 | 15,90 s |
| *source 720p, antes de ampliar* | *1.991* | — | — |

Duas conclusoes mudaram a escolha:

1. **o PRESET domina o CRF** — veryfast→medium rende +3,3%, crf20→18 rende +1,5%;
2. **`slow` nao rende nada sobre `medium`** (1.924 contra 1.931, dentro do ruido) e custa 3x o
   tempo. A hipotese era slow/crf18; a medicao parou em medium/crf18.

Honestidade sobre o ganho: +3,8% no quadro, e a olho nu nao distingui os frames (SSIM entre o
antigo e o melhor esforco: 0,9927; PSNR 47 dB). Mudou porque custa pouco — +2,2 s por Short,
~9 s num pedido de quatro contra ~86 s de download. **Isso NAO resolve a percepcao de "imagem
mole"**, que vem da ampliacao de 720p, nao do encode.

#### Revisao de 2026-07-29: o CRF voltou para 20 (o preset ficou em medium)

A tabela acima decidiu com METADE da conta: ela mede o que se GANHA em nitidez e o que se paga em
TEMPO — e nao mede o que se paga em DISCO. Medido agora nos 4 Shorts reais do culto xZNTJcehAV0,
renderizados pelo `cmd/render` nos dois CRF:

| short | crf18 | crf20 | diferenca |
|---|---|---|---|
| 01 | 41,7 MB | 33,2 MB | -20,4% |
| 02 | 46,1 MB | 36,9 MB | -20,0% |
| 03 | 48,9 MB | 38,8 MB | -20,6% |
| 04 | 31,0 MB | 24,8 MB | -20,0% |
| **total** | **167,7 MB** | **133,7 MB** | **-20,3%** |

E a diferenca de IMAGEM entre as duas saidas, no mesmo material:

| medida | resultado (short 01 · 02 · 03 · 04) |
|---|---|
| laplaciano no quadro de 5 s | 94,23/94,23 · 102,77/102,91 · 118,14/118,05 · 107,20/107,68 |
| SSIM (Y) crf18 vs crf20 | 0,9898 · 0,9909 · 0,9900 · 0,9904 |
| PSNR (Y) crf18 vs crf20 | 44,8 · 45,6 · 44,9 · 45,2 dB |

Em **dois dos quatro** o crf20 mediu laplaciano MAIS ALTO que o crf18 — o que confirma que os
+1,5% da tabela original eram ruido, e nao uma ordem de qualidade. PSNR de ~45 dB e diferenca
abaixo do limiar de percepcao.

Conclusao: **o crf18 comprava 20% de arquivo por um ganho invisivel.** E em conteudo que ja e
ampliacao de 720p macio, bit extra preserva a MACIEZ da fonte, nao detalhe que nao existe.

Nao e decisao de tamanho para envio (aquele limite era falso, ver a Parte 4): e qualidade contra
disco, e a qualidade empatou. O preset fica em `medium`, onde o ganho e real.

Tempo de render: 26,1 s (crf18) contra 28,0 s (crf20) para os 4 Shorts — inverso do esperado e
dentro da variacao da maquina; nao entrou na decisao.

### Degrade do rodape: mais gradual

De 1200/1.00 para **1400/0.80** — rampa mais alta e opacidade maxima menor, para o pregador nao
desaparecer atras da base escura. Frames comparativos em `docs/mockups/rodape/`.

Achado ao mudar: o `cmd/servidor` fixava `RodapeAlpha: 1.00` no codigo, entao a constante
`rodapeAlphaPadrao` do pacote era **letra morta** no caminho que o operador usa. Agora o servidor
deixa zerado e herda o padrao medido.

### Registro dos cortes: acumular o dado, sem agir sobre ele

Cada ajuste manual e uma **medicao** do desvio da legenda: o operador, ao empurrar as pontas ate
soar certo, esta medindo quanto o carimbo se adianta ao audio.

`resultados/cortes.csv` grava **uma linha por trecho APROVADO** -- ajustado ou nao:
`quando, pedido, indice, ajustado, start_original, start_final, delta_start_ms, end_original,
end_final, delta_end_ms, duracao_original_s, duracao_final_s`. Modo append, cabecalho na
criacao, serializado pelo mesmo mutex do log de rodadas.

**Por que TODOS os aprovados, e nao so os ajustados.** Registrar apenas os ajustados montaria
uma amostra composta so dos casos ruins -- o trecho aprovado sem ajuste e justamente a evidencia
de que o corte estava BOM, e nao geraria linha. O erro nao e marginal:

| | media sobre os ajustados | media real |
|---|---|---|
| 10 aprovados, 3 ajustados em +2 s, 7 aceitos | **2,0 s** | **0,6 s** |

Aplicar 2 s na Fase 3 empurraria os 7 cortes corretos para longe demais: o remedio criaria a
doenca nos casos saudaveis. Com os nao ajustados dentro (delta 0), a media fica correta e sai de
graca a **proporcao de cortes que precisam de ajuste** -- o indicador de saude do sistema. Se
cair de 60% para 10%, melhorou.

Daí o arquivo chamar-se `cortes.csv` e nao `ajustes.csv`: um arquivo chamado "ajustes" convida
quem o le a filtrar mentalmente so os ajustados, recriando o vies que a mudanca removeu.

A coluna `ajustado` reflete se o corte **mudou**, nao se o operador mexeu no painel: quem
experimenta e volta ao original esta confirmando que o corte estava bom, e contar isso como
"precisou de ajuste" estragaria o indicador. Trecho **reprovado** nao entra -- ali o operador
rejeitou o CONTEUDO, nao o recorte, e misturar as duas coisas na mesma coluna nao mede nada.

#### Dois cuidados para quando formos ler os numeros

**(a) Os deltas sao QUANTIZADOS por fronteira de frase.** O corte encaixa em fala, nao em tempo
continuo, entao a distribuicao sai **aos caroços**, agrupada nas distancias tipicas entre
frases. Com poucas amostras a media cai num vale entre dois caroços e nao descreve nenhum caso
real. Olhar a **forma** da distribuicao, nao so o valor central.

**(b) O delta mede a soma de DOIS efeitos:** o adiantamento da legenda (fisica da fonte) e a
preferencia do operador por respiro no corte (gosto). Aplicar o total na Fase 3 embutiria o
gosto dele como se fosse fisica da fonte -- e a Fase 3 vale para todos os pregadores e todos os
cultos, inclusive quando quem revisa for outra pessoa. Pista para separar: se inicio e fim
andarem com magnitudes parecidas, e sincronia; se o fim andar sistematicamente mais que o
inicio, tem gosto no meio.

**Deliberadamente sem correcao automatica e sem sugestao de vies.** Agir sobre tres pontos seria
construir sobre ruido. Depois de uns dez trechos, olha-se se o desvio e consistente: se for,
aplica-lo na **Fase 3** melhoraria todos os cortes de uma vez e tornaria o ajuste manual excecao
em vez de rotina; se nao for, o dado custou nada e a hipotese morre com evidencia em vez de
opiniao.

Falha ao gravar nunca quebra o pedido -- e dado de pesquisa, e o Short do operador vale mais que
a estatistica (coberto por teste).

### Dois cuidados tecnicos do estudo de bibliotecas

O estudo foi descartado no geral (as bibliotecas exigem midia local, que a revisao nao tem --
ali o video e o player do YouTube). Duas recomendacoes valeram:

- **`seekTo(t, false)` durante os empurroes e `seekTo(t, true)` quando o debounce assenta.**
  E recomendacao da documentacao do player: `allowSeekAhead=false` so reposiciona, sem disparar
  requisicao de video. Sem isso, uma rajada de cliques dispara uma requisicao por clique.
- **Tempos guardados como inteiros em milissegundos**, nunca strings nem floats. Uma sequencia
  de empurroes de 0,25 s em float de segundos acumula erro, e o tempo e a chave do corte. O
  contrato do endpoint passou a ser `start_ms`/`end_ms` inteiros; a conversao para o formato
  `HH:MM:SS.000` do Candidato acontece so na saida. Um teste simula 8 empurroes de 250 ms e
  exige o valor exato.

### Convergencia com a spec-14 (confronto doutrinario) -- REGISTRADO, nao implementado

O confronto com contexto produz a classe `ambiguo_isolado` -- "o trecho e ambiguo sozinho,
mas o sermao resolve" --, cuja acao natural e **estender o corte**, e o campo
`onde_resolve.frase` diz **ate onde**. Ou seja: **o confronto diz que o corte ficou curto;
este ajuste permite consertar.**

O desenho ja esta preparado para a sugestao pre-preencher o ajuste. O que falta e so a ponte,
quando a spec-14 existir:

1. localizar `onde_resolve.frase` na transcricao (`harness.AcharAncora`, com o desfecho de
   busca aproximada que a spec-14 define para quando a frase nao casa);
2. propor `end` = fim daquela frase, e chamar o mesmo `POST /ajustar` que o operador usa;
3. se a extensao estourar a faixa de duracao, a guarda ja recusa com o numero -- a sugestao
   nao precisa saber calcular viabilidade, so propor.

Nada disso exige mudanca no que foi implementado aqui: a sugestao entra como um pre-
preenchimento do mesmo fluxo, e a decisao continua sendo do operador.

### Criterios de aceite da v2

- [x] `POST /ajustar` devolve hook, `duration_seconds` e o texto realmente falado da janela
      nova, via `harness.Frasear`.
- [x] O hook e recalculado pela regra da Fase 3 (primeira frase a partir do start final).
- [x] `POST /aprovar` aceita os tempos ajustados por trecho, e sao esses que vao ao render.
- [x] As duas pontas livres para frente com folga limitada; encaixe so para tras. No inicio, o
      hook e a frase que CONTEM o start. As invariantes do `cmd/auditar` reformuladas nas duas
      pontas (spec-16), sem excecao para o ajuste manual.
- [x] Guardas no SERVIDOR: faixa de duracao (fonte unica na `harness`), `end <= start`, clamp
      nos limites da pregacao; mensagens com os numeros.
- [x] Controles: faixa de frases clicavel; botao que NOMEIA o tempo do player; rotulos pelo
      efeito ("mais cedo"/"mais tarde") com o valor entre as setas; ajuste fino subordinado so
      no Fim; "ouvir o fim" junto dos controles do Fim; restaurar original; meia velocidade.
- [x] Sem falsa precisao na tela: segundos inteiros e `HH:MM:SS`, nunca `54.75s` nem `.000`.
- [x] `seekTo(t, false)` nos empurroes e `seekTo(t, true)` ao assentar o debounce.
- [x] Tempos internos em milissegundos INTEIROS, no cliente e no servidor.
- [x] Feedback ao vivo com debounce atualizando duracao, hook e texto falado no card.
- [x] `resultados/cortes.csv` registra TODOS os trechos aprovados (delta 0 nos nao ajustados) com
      coluna `ajustado`, para a media nao enviesar e a proporcao sair de graca.
- [x] Testes: recalculo, `/aprovar` usando os ajustados e nao os originais, guardas de faixa
      e de `end <= start`, fluxo ponta a ponta no formato que o cliente envia, e o contrato da
      tela redesenhada (rotulos pelo efeito, ausencia de "Marcar aqui", vizinhanca com contexto
      dos dois lados e presente mesmo no ajuste invalido, sem falsa precisao, `seekTo` correto,
      tempos em ms).
- [x] `go build`, `go vet` e `go test -race ./...` verdes.

## Critérios de aceite da v3

**Telas e navegação**
- [ ] As quatro telas (`dados`, `processando`, `revisao`, `resultado`) existem no DOM desde o
      primeiro carregamento; a troca é `hidden`, no cliente.
- [ ] Trocar de tela **não gera requisição**: nenhum elemento do indicador de etapas tem
      atributo `hx-*`, e `irPara` não chama `fetch`/`htmx.ajax`. Verificado por teste sobre o
      template, não por inspeção visual.
- [ ] O indicador de etapa mostra `atual` / `alcançada` / `bloqueada` / `erro`, e etapa não
      alcançada não é clicável.
- [ ] A tela `processando` mostra as DUAS linhas (legenda e seleção) com estado e tempo.
- [ ] Ver etapa anterior nunca pede confirmação e nunca perde a revisão em andamento.
- [ ] Ação destrutiva (buscar outros trechos, nova janela, gerar de novo) confirma **nomeando
      o que se perde** (nº de aprovações e de ajustes).
- [ ] Não existe botão "refazer a seleção" com temperatura 0.

**F5**
- [ ] `GET /pedido-atual` devolve 204 sem pedido, e o payload de status com pedido.
- [ ] Abrir a página com pedido em cada estado cai na tela correspondente (tabela da v3).
- [ ] Com decisão de revisão não enviada, sair da página avisa (`beforeunload`).

**Largura**
- [ ] `.folha` e o rodapé fixo em 1180px; escala de espaçamento em variáveis; faixa de frases
      com 560px de altura máxima; uma coluna abaixo de 760px.

**Resultado** (Parte 4 — implementada; ver a seção adiante)
- [x] Cada Short tem `<video>` embutido que toca o arquivo LOCAL (não o embed do YouTube),
      tamanho vindo do servidor, duração vinda do próprio player, baixar e apagar.
- [x] Apagar confirma nomeando o arquivo e remove só o arquivo (o `cortes.csv` não muda).
- [x] O endpoint de apagar recusa nome fora da whitelist (mesma função do download), com teste —
      e o teste do download foi corrigido, porque passava com a whitelist removida.
- [x] A tela diz, em uma linha, que o envio é baixar + WhatsApp Web. **Não** existe botão de
      compartilhar.

**Cache por vídeo**
- [ ] `videos/<idDoVídeo>/` guarda `video.mp4`, `video.json`, `legenda.srt`,
      `legenda.info.json` e a `transcricao.txt` do vídeo INTEIRO.
- [ ] `trabalho/<idDoPedido>/` guarda `pedido.json` (com `video_id`), a transcrição
      RECORTADA à janela e `candidatos.corrigido.json`.
- [ ] Dois pedidos do mesmo vídeo com **janelas diferentes**: um único download, dois
      `candidatos.corrigido.json` distintos, nenhum sobrescrito.
- [ ] Acerto de cache **não chama** o baixador de vídeo (teste), e a fase pesada cai de ~35 s
      de download para 0.
- [ ] O id do vídeo **não** é usado como nome de pasta de pedido.
- [ ] `download.VideoID` cobre `watch?v=`, `youtu.be/`, `&t=`, `&list=`, `/live/<id>`,
      `/embed/`, `/shorts/`, `m.youtube.com`, host em maiúsculas; **rejeita** URL de outro
      site e entrada hostil (`../../etc`), validando `^[A-Za-z0-9_-]{11}$` antes de virar
      caminho.

**Origem (spec-09)**
- [ ] `videocache.Localizar` é o único lugar que resolve arquivo + origem; vídeo na pasta do
      pedido vence o do cache; sem nenhum dos dois, erro claro.
- [ ] `cmd/render` sobre pedido cujo vídeo está no CACHE renderiza a **cena certa**,
      verificado por conteúdo de frame (reusar `internal/video/origem_do_video_test.go`).

**Geral**
- [ ] `go build ./...`, `go vet ./...` e `go test -race ./...` verdes.

## O fluxo invertido deixa de valer para o VÍDEO (decisão de 2026-07-30)

Registrado aqui para ninguém "corrigir" isso depois achando que é regressão.

A razão original de baixar o vídeo **depois** da aprovação era não gastar ~570 MB e ~60 s quando a
seleção não presta: primeiro o operador vê os candidatos, e só o que ele aprova custa download.

A v4 muda a troca, porque **o vídeo passou a ser insumo da própria revisão**: é dele que saem as
pausas de fala (a fronteira do corte) e é ele que o operador vai ouvir no player local, em vez do
YouTube. Sem o arquivo, a revisão volta a decidir por fronteira de legenda — que é o bug medido.

A conta também mudou: com o **cache por vídeo**, 31 de 46 pedidos medidos reaproveitam o vídeo, e
o download só acontece no culto novo. E ele pode rodar **em paralelo com a seleção** (rede contra
GPU, ~63 s escondidos atrás de ~29 s).

Então: o fluxo invertido continua valendo para a LEGENDA (3 s, sempre antes) e deixa de valer para
o vídeo. O que se perde é banda num culto cuja seleção o operador vá descartar inteira; o que se
ganha é o corte cair onde a fala termina.

## Parte 4 implementada (2026-07-29) — tela de resultado

### Player do ARQUIVO LOCAL, não o embed do YouTube

Requisito explícito do dono, e a razão é o que o operador precisa ver aqui: o **produto** —
1080x1920, logo e gradiente de rodapé queimados. O embed do YouTube mostraria a FONTE, que é
justamente o que ele acabou de ver na revisão. Então `<video controls preload="metadata"
src="/finalizados/<pedido>/<short>.mp4">`.

`preload="metadata"` carrega duração e primeiro frame sem baixar os 40 MB.

### A duração vem do player; o tamanho, do servidor

- **Duração:** quem sabe dizê-la é o arquivo, e o player a mostra sozinho (`0:00 / 0:37`).
  Calculá-la a partir do candidato aprovado seria repetir uma conta que o vídeo já responde — e
  mentiria se o render tivesse ajustado qualquer coisa.
- **Tamanho:** vem no estado (`shorts[].bytes`) porque o cliente não tem como saber sem baixar. É
  **informação de contexto**, sem limiar e sem cor.

`shorts` virou **uma** lista de objetos (`{nome, bytes}`), não uma lista de nomes mais uma de
tamanhos — duas listas para o mesmo dado divergem (foi o que quebrou o cabeçalho do `tempos.csv`
duas vezes).

### O limite de 16 MB do WhatsApp não existe na prática — aviso REMOVIDO

A primeira versão do cartão pintava o peso de vermelho acima de 16 MB, dizendo que o WhatsApp
recusaria o arquivo. **É falso, e o dono mediu:** um Short de **41,7 MB enviado pelo WhatsApp sem
problema**.

O aviso saiu. Duas razões, e a segunda é a que importa mais:

1. desaconselhava um fluxo que **funciona**;
2. **aviso errado é pior que aviso nenhum** — ensina o operador a ignorar avisos, inclusive os
   certos. Um alerta só se paga se quem o lê puder confiar nele.

**Consequência direta: nenhuma das três "saídas" que o relatório anterior propôs é necessária** —
não há por que subir o CRF *por causa de tamanho*, nem criar uma segunda saída "leve", nem mandar
por link. **Não se degrada a saída para resolver um problema que não existe.**

Os números medidos ficam registrados como **fato de tamanho**, sem juízo: os Shorts do culto medido
têm 39,8 MB (37 s) e 29,6 MB (30 s), ~1,1 MB/s de vídeo em 1080x1920.

**Quem envia é o operador, sempre.** A regra inviolável 7 do CLAUDE.md fechou isso do lado do
agente: ele não opera canal de comunicação da igreja, nem para testar — o que sai por ali sai em
nome da igreja, para pessoas reais, e não tem desfazer.

### Apagar: uma whitelist, dois verbos

`DELETE /finalizados/{id}/{arquivo}` e o `GET` de download chamam a MESMA função
(`caminhoDoShort`): endpoint que apaga com travessia de caminho é muito pior que um que baixa, e
duas cópias da checagem seriam duas chances de uma ficar para trás.

- Apaga o **arquivo** e tira o nome do inventário em memória (que é o que a tela lista e o que a
  whitelist autoriza). Segundo clique dá 404, e o download do mesmo nome também.
- **Não toca no `cortes.csv`** (decisão do dono): aquilo é medição do desvio da legenda, não
  inventário de entrega. Apagar um Short não desfaz o corte ter sido aprovado naqueles tempos.
- A **confirmação é da tela** e nomeia o arquivo. Endpoint não confirma nada — quem clica é quem
  vê o nome.
- A resposta é o mesmo fragmento de estado das outras rotas: a tela se repinta com a lista já sem
  o arquivo, em vez de o cliente remover o cartão por conta própria e passar a discordar do
  servidor sobre o que existe em disco.

**Lição de teste (mutação):** o `TestBaixarFinalRecusaArquivoForaDaWhitelist` **passava com a
whitelist removida**. Ele pedia `segredo.txt`, que não existia — o 404 vinha do `http.ServeFile`
não achar o arquivo, não da guarda. Corrigido para criar o arquivo de verdade e conferir que o
conteúdo não é servido. É o sexto teste deste projeto que passava com o bug presente, e o padrão é
sempre o mesmo: medir o sintoma conveniente.

### Compartilhar: sem botão falso

A tela diz em uma linha: **baixe o arquivo e mande pelo WhatsApp Web.** Sem integração, porque não
há caminho bom (Web Share API com arquivo é instável fora do celular, e um botão que abrisse o
WhatsApp sem anexar o vídeo prometeria o fluxo e entregaria meia ação).

## Como validar a v3

```bash
# 1) navegação sem requisição e integridade das referências de JS
go test ./internal/servidor/ -run 'Telas|Etapas|Referencias' -v

# 2) cache: dois pedidos, mesmo vídeo, janelas diferentes -> um download só
go test ./internal/servidor/ ./internal/videocache/ -v

# 3) origem com vídeo no cache (verificação por CONTEÚDO do frame)
go test ./internal/video/ -run Origem -v

# 4) ponta a ponta real, medindo o ganho do cache
go run ./cmd/servidor -porta 7799 -sublang pt-orig
#    primeiro pedido: baixa (~35 s de vídeo). segundo pedido, MESMO culto, outra janela:
#    a fase pesada não baixa nada.
tail -2 resultados/tempos.csv    # comparar a coluna baixar_video_s: ~35 -> ~0
du -sh videos/*                  # um diretório por culto, não por pedido
```

## Nota

Esta interface e a porta de entrada do operador leigo. Com ela, o pipeline (specs 02-04,
07-13) deixa de exigir terminal. E conveniencia sobre um produto que ja esta pronto para
publicar.