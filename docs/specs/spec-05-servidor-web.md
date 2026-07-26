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
- **v1 = aprovar/reprovar SEM ajuste fino de corte** (decisao do dono). O ajuste fino
  (marcar inicio/fim ouvindo, via IFrame API) fica para a v2 -- ver "Futuro (v2)".
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
- Ajuste fino de corte pelo operador (marcar inicio/fim) -- e a v2 (abaixo).
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
o corte. A correspondencia exata player<->arquivo so se torna critica na v2 (ajuste fino).

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
    considere estender". NAO e alerta de doutrina: e convite a ajustar o trecho (liga com o
    ajuste fino do corte, v2 abaixo).
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
- [ ] O start/end do corte corresponde ao mesmo instante mostrado no player.
- [ ] Erro em qualquer fase aparece na pagina com mensagem clara.
- [ ] Testes: rotas (httptest), maquina de estados das duas fases com download/selecao/
      render mockados; validacao de entrada. Nao subir pipeline real nos testes.
- [ ] go build ./... e go test ./... verdes.

## Futuro (v2) -- ajuste fino de corte pelo operador

Registrado (pesquisa do dono): usar a YouTube IFrame Player API para o operador aparar
inicio/fim ouvindo. player.seekTo(seg, true) aceita fracoes (ex.: +-0.033s = 1 frame a
30fps); player.getCurrentTime() captura o instante exato onde o operador marca; botoes
"marcar inicio/fim", "+-1 frame", setPlaybackRate para revisao lenta. O tempo capturado
vira o start/end do corte. CUIDADO a resolver na v2: o seekTo do YouTube pode saltar
para keyframe (nao frame exato) e o player e OUTRO video que nao o arquivo baixado -- e
preciso garantir que o tempo marcado no player corresponda ao mesmo instante no arquivo
baixado (mesma origem t=0), senao o corte sai deslocado. Resolve, de forma humana, tanto a
entonacao (voz que nao fecha) quanto o timestamp impreciso da legenda.

**Convergencia com a spec-14 (confronto doutrinario):** o confronto com contexto produz a
classe `ambiguo_isolado` — "o trecho e ambiguo sozinho, mas o sermao resolve" —, cuja acao
natural e **estender o corte**. Ou seja: **o confronto diz que o corte ficou curto; este
ajuste manual permite consertar.** Sem o ajuste, o operador so pode REPROVAR um trecho cujo
conteudo e bom, o que e desperdicio. As duas frentes se encaixam e valem em sequencia.

## Nota

Esta interface e a porta de entrada do operador leigo. Com ela, o pipeline (specs 02-04,
07-13) deixa de exigir terminal. E conveniencia sobre um produto que ja esta pronto para
publicar.