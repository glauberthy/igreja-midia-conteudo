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
5. **Fase pesada (baixar-video + cortar)**: so agora o sistema baixa o video (idealmente
   so os trechos aprovados, via --download-sections do yt-dlp, se viavel; senao o video
   inteiro) e renderiza (corte + 9:16 + legenda + logo). Status: baixando-video,
   renderizando, concluido.
6. **Entrega**: a pagina lista os Shorts de finalizados/<id>/ para baixar. O operador
   envia por WhatsApp Web (fora do sistema).

## Escopo

Dentro:
- cmd/servidor (HTTP, porta configuravel, sem auth) + pagina unica servida via embed.
- Orquestracao em duas fases separadas por aprovacao humana:
  - fase leve: baixar-legenda -> Selecionar (harness);
  - (pausa: aprovacao do operador);
  - fase pesada: baixar-video (so aprovados, se viavel) -> Renderizar.
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
Referencia visual/codigo: `docs/mockups/revisao-trechos_referecnia.html`.

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
- **Alerta doutrinario em duas camadas, com tres niveis de intensidade.** Ambar na trilha
  (o operador ve antes de chegar que o trecho pede atencao) e uma faixa no card com o
  `motivo_revisao` em texto. Nunca esconder trechos marcados. Mas para nao cair em fadiga
  de alerta (⚠ em tudo deixa de ser alerta), a INTENSIDADE segue a classe do confronto
  doutrinario (spec-14):
  - `desalinhamento` -> **alto**: ⚠ ambar, destaque forte, com o ponto citado da Declaracao.
  - `provavel_erro_transcricao` -> **baixo**: ℹ neutro/quieto (cinza), "conferido: provavel
    erro de transcricao".
  - `fiel` (marcado pela Fase 4, confronto nao achou) -> **baixo**: ℹ neutro/quieto,
    "conferido: sem problema doutrinario aparente".
  - sem confronto ainda (spec-14 nao implementada) -> ⚠ ambar generico (comportamento atual).
  O operador ve TODOS os marcados, mas sabe onde olhar primeiro. Enquanto a spec-14 nao
  existe, so ha o nivel generico; os tres niveis entram junto com ela.
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

## Criterios de aceite

- [ ] Servidor sobe na porta configuravel (padrao :7799), sem auth; GET / serve a pagina.
- [ ] POST /pedidos valida entrada, cria o pedido e dispara a fase leve; responde id na
      hora (nao bloqueia).
- [ ] A fase leve baixa SO a legenda (nao o video) e roda a selecao; sem legenda -> erro.
- [ ] A pagina lista os candidatos, cada um com player YouTube embutido tocando de start a
      end, hook/dur/score, alerta de revisao quando marcado, e botoes aprovar/reprovar.
- [ ] POST /pedidos/{id}/aprovar dispara a fase pesada so para os aprovados.
- [ ] A fase pesada baixa o video (so aprovados, se viavel) e renderiza; a pagina lista os
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

## Nota

Esta interface e a porta de entrada do operador leigo. Com ela, o pipeline (specs 02-04,
07-13) deixa de exigir terminal. E conveniencia sobre um produto que ja esta pronto para
publicar.