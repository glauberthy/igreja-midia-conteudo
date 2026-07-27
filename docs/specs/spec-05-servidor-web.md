# Spec 05 — Interface web do operador (fluxo invertido: selecionar antes de baixar)

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
- **Uma tela** conduzindo todas as etapas (decisão do dono): cola link+tempos -> processa
  -> lista trechos com player para revisar -> aprova/reprova -> baixa e corta os aprovados.
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
  a janela contigua por decisao de medicao (abaixo). Contrato: `origemVideoCompleto = 0` — o
  arquivo comeca no inicio do video, entao o render corta em tempo ABSOLUTO, sem calculo de
  origem a propagar. `download.BaixarVideoCompleto`.

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

- **Alinhamento de tempo (como ficou).** Servidor: video inteiro -> `origemVideoCompleto = 0`
  -> o render corta em tempo ABSOLUTO. CLI (`cmd/baixar` + `cmd/render`): o video.mp4 e a
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
video INTEIRO com origem 0 (`origemVideoCompleto`), e o player do YouTube tambem conta do
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

## Nota

Esta interface e a porta de entrada do operador leigo. Com ela, o pipeline (specs 02-04,
07-13) deixa de exigir terminal. E conveniencia sobre um produto que ja esta pronto para
publicar.