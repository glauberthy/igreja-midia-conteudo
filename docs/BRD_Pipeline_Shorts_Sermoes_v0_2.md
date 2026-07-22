**DOCUMENTO DE REQUISITOS DE NEGÓCIO · BRD**

**Pipeline de geração de Shorts a partir de sermões**

Definição do comportamento de negócio para seleção, edição e geração de vídeos verticais a partir de cultos publicados no YouTube

**Versão:** 0.1

**Status:** Rascunho para análise e validação

**Data:** 20 de julho de 2026

**Responsável:** A definir

> **Propósito:** Formalizar as regras de negócio do pipeline antes da definição da arquitetura, da escolha definitiva dos modelos de IA e da abertura do backlog de desenvolvimento.

# 1. Finalidade do documento

Este documento descreve o comportamento esperado de um pipeline destinado a transformar a gravação completa de um culto em Shorts focados exclusivamente na ministração da Palavra. Ele estabelece objetivos, limites, atores, regras de seleção de conteúdo, controles de qualidade e critérios de aceite.

O BRD não define ainda a implementação detalhada, a arquitetura de software ou o fornecedor definitivo de LLM. Essas decisões deverão respeitar as regras registradas aqui e poderão ser documentadas posteriormente em uma Especificação Funcional e em um Documento de Arquitetura.

# 2. Contexto e problema de negócio

Os cultos da igreja são transmitidos ao vivo e permanecem publicados no YouTube. Após o encerramento da transmissão, a plataforma disponibiliza legendas automáticas em português brasileiro. O vídeo completo reúne diferentes atividades, como louvor, avisos, orações e pregação, mas o conteúdo destinado aos Shorts deverá considerar somente a ministração da Palavra.

A identificação manual de bons trechos, o reenquadramento para vídeo vertical e a preparação de legendas estilizadas consomem tempo e dependem de edição especializada. O pipeline deverá reduzir esse trabalho sem retirar a responsabilidade humana pela aprovação do conteúdo publicado.

# 3. Objetivos

## 3.1 Objetivo principal

Gerar, de forma assistida e repetível, vídeos curtos verticais derivados de sermões, preservando o contexto bíblico e pastoral, a identidade visual da igreja e a qualidade mínima necessária para publicação como YouTube Short.

## 3.2 Resultados esperados

- Reduzir o tempo empregado na localização manual dos melhores momentos da pregação.

- Evitar a seleção de falas incompletas, descontextualizadas ou inadequadas para consumo isolado.

- Padronizar duração, proporção, resolução, legendas e identidade visual dos Shorts.

- Permitir o uso de LLM local, LLM externo por API ou combinação controlada dos dois.

- Manter revisão humana antes da publicação do conteúdo.

# 4. Escopo

## 4.1 Incluído no escopo

- Receber a URL de um culto publicado no YouTube.

- Receber ou determinar o intervalo de início e fim da pregação.

- Baixar o vídeo e a legenda automática em português brasileiro.

- Extrair, limpar e normalizar somente a legenda correspondente à pregação.

- Analisar a transcrição completa da pregação com LLM.

- Gerar, justificar e classificar candidatos a Short.

- Permitir revisão e aprovação humana de um candidato.

- Recortar, reenquadrar, legendar e renderizar o vídeo aprovado em formato vertical 9:16.

## 4.2 Fora do escopo inicial

- Publicação automática no YouTube, Instagram ou outras redes sociais.

- Geração automática de títulos, descrições, hashtags e capas para publicação.

- Análise multimodal de uma hora inteira de vídeo por modelo externo.

- Substituição da revisão pastoral ou editorial por decisão exclusiva da IA.

- Edição cinematográfica avançada, inclusão automática de imagens externas ou trilhas musicais.

# 5. Atores e responsabilidades

| **Ator**              | **Responsabilidade**                                                                                                                   |
|-----------------------|----------------------------------------------------------------------------------------------------------------------------------------|
| Operador/editor       | Informa a URL e os limites da pregação, acompanha o processamento, revisa os candidatos e solicita a renderização.                     |
| Aprovador de conteúdo | Valida se o trecho preserva o contexto bíblico, pastoral e institucional antes da publicação.                                          |
| Pipeline              | Orquestra download, preparação da legenda, análise, validações, recorte, legendagem e renderização.                                    |
| LLM local             | Analisa a transcrição usando infraestrutura local e pode atuar como mecanismo principal, alternativo ou gerador inicial de candidatos. |
| LLM externo           | Analisa ou revisa os candidatos por API, conforme o modo configurado e as regras de privacidade.                                       |

# 6. Termos e definições

| **Termo**               | **Definição**                                                                                                     |
|-------------------------|-------------------------------------------------------------------------------------------------------------------|
| Intervalo da pregação   | Período do vídeo compreendido entre o início e o fim da ministração da Palavra.                                   |
| Transcrição normalizada | Legenda da pregação sem duplicações ou marcações desnecessárias, mantendo texto e timestamps rastreáveis.         |
| Candidato               | Trecho sugerido pelo LLM como possível Short, contendo início, fim, pontuação, justificativa e frase de abertura. |
| Short                   | Vídeo final vertical, em proporção 9:16, derivado de um candidato aprovado.                                       |
| Modo local              | Processamento de linguagem executado exclusivamente por um modelo hospedado localmente.                           |
| Modo externo            | Processamento de linguagem executado por um modelo acessado por API.                                              |
| Modo híbrido            | Combinação de modelo local e externo em etapas distintas de geração e revisão.                                    |
| Perfil visual           | Conjunto versionado de fonte, cores, tamanho, posição, margens e comportamento das legendas.                      |

# 7. Visão geral do processo

| **Etapa** | **Nome**    | **Resultado**                                                             |
|-----------|-------------|---------------------------------------------------------------------------|
| 1         | Entrada     | Registrar URL, início e fim da pregação e perfil visual.                  |
| 2         | Aquisição   | Baixar vídeo, metadados e legenda em português.                           |
| 3         | Preparação  | Isolar e normalizar a transcrição completa da pregação.                   |
| 4         | Compreensão | Fazer o LLM ler toda a transcrição e mapear tema, estrutura e aplicações. |
| 5         | Seleção     | Gerar e classificar candidatos com até 60 s (alvo de 58 s).                       |
| 6         | Revisão     | Apresentar os melhores candidatos para aprovação humana.                  |
| 7         | Produção    | Recortar, reenquadrar, sincronizar legendas e renderizar.                 |
| 8         | Entrega     | Disponibilizar o Short e preservar informações de rastreabilidade.        |

# 8. Entradas e saídas

## 8.1 Entradas obrigatórias

- URL pública ou acessível do vídeo no YouTube.

- Horário inicial e final da pregação, no MVP.

- Modo de LLM: local, externo ou híbrido.

- Perfil visual de legenda e enquadramento.

## 8.2 Saídas

- Mapa resumido do sermão e temas identificados.

- Lista classificada de candidatos com timestamps e justificativas.

- Prévia dos candidatos definidos para revisão.

- Arquivo final em MP4 vertical 9:16 com legenda incorporada.

- Registro do processamento: fonte, intervalos, modelo, prompt, decisão humana e versão gerada.

# 9. Regras de negócio

> **Convenção:** As regras identificadas como RN-xxx são obrigatórias para o MVP, salvo quando marcadas como evolução ou decisão pendente.

## 9.1 Origem e delimitação do conteúdo

**RN-001 — Origem autorizada**  
O pipeline somente deverá processar vídeos próprios da igreja ou conteúdos para os quais exista autorização de uso e edição.

**RN-002 — URL obrigatória**  
Cada processamento deverá estar associado a uma URL de origem válida e identificável.

**RN-003 — Delimitação manual no MVP**  
O operador deverá informar o início e o fim da pregação. A detecção automática desse intervalo será tratada como evolução posterior.

**RN-004 — Validação do intervalo**  
O início deverá ser anterior ao fim, ambos deverão estar dentro da duração do vídeo e o intervalo deverá possuir conteúdo suficiente para análise.

**RN-005 — Exclusão das demais atividades**  
Louvor, avisos, orações, apresentações e demais partes do culto fora do intervalo informado não deverão participar da seleção dos Shorts.

**RN-006 — Idioma prioritário**  
A legenda em português brasileiro deverá ser priorizada. Na ausência de legenda aceitável, o processamento deverá ser interrompido; o MVP não realizará transcrição local.

**RN-007 — Conteúdo de terceiros no trecho**  
O sistema deverá sinalizar a presença de música, trilha, imagem projetada ou outro conteúdo de terceiros dentro do trecho selecionado, para que o operador avalie o risco de reivindicação de direitos autorais (Content ID) antes da publicação.

## 9.2 Preparação e rastreabilidade da transcrição

**RN-010 — Recorte textual**  
Somente as entradas da legenda que coincidirem com o intervalo da pregação deverão compor a transcrição enviada para análise.

**RN-011 — Normalização**  
O pipeline deverá remover duplicações progressivas, marcações técnicas e ruídos da legenda automática sem alterar o sentido da fala.

**RN-012 — Preservação dos timestamps**  
Cada segmento textual deverá manter referência ao horário original no vídeo para permitir validação e recorte precisos.

**RN-013 — Integridade**  
A normalização não deverá resumir, reescrever ou corrigir teologicamente o conteúdo antes da análise do LLM.

**RN-014 — Rastreabilidade**  
Deverão ser preservadas a legenda original obtida e a versão normalizada utilizada na análise.

## 9.3 Compreensão integral do sermão

**RN-020 — Leitura integral**  
O LLM deverá receber e analisar toda a transcrição normalizada da pregação antes de indicar qualquer candidato.

**RN-021 — Mapeamento prévio**  
Antes da seleção, o LLM deverá identificar o tema central, a estrutura geral, os principais argumentos, aplicações e conclusões do sermão.

**RN-022 — Especialização por instrução**  
A análise deverá utilizar uma instrução de sistema versionada, especializada em sermões cristãos e edição de vídeos curtos. A instrução deverá incorporar os parâmetros doutrinários da igreja (confissão de fé ou declaração doutrinária de referência), de modo que a seleção respeite a leitura teológica adotada.

**RN-023 — Contexto completo**  
Um candidato não poderá ser avaliado apenas por impacto verbal; deverá ser confrontado com o contexto geral da pregação.

**RN-024 — Fidelidade pastoral**  
O sistema não deverá favorecer um trecho que, isoladamente, altere a intenção do pregador, produza ambiguidade grave ou transforme uma afirmação contextual em conclusão absoluta.

**RN-025 — Limite da análise textual**  
A seleção baseada na legenda avaliará conteúdo e estrutura textual. Tom de voz, qualidade do áudio, enquadramento e expressão visual deverão ser verificados posteriormente na prévia.

**RN-026 — Integridade da citação bíblica**  
Quando o trecho contiver leitura, citação ou paráfrase da Escritura, o corte deverá preservar a citação completa e a referência associada (livro, capítulo e versículo, quando mencionados), sem interromper o versículo no meio nem separá-lo da aplicação feita pelo pregador.

**RN-027 — Proibição de recorte de posição refutada**  
O sistema não deverá selecionar trecho em que o pregador apresente uma objeção, um erro, uma heresia ou uma posição contrária apenas para refutá-la em seguida, quando o corte incluir a posição apresentada sem a refutação correspondente. A conclusão do pregador sobre o ponto deverá estar contida no trecho.

**RN-028 — Preservação de ressalvas e qualificadores**  
O corte não deverá remover condicionais, negações e qualificadores ("mas", "porém", "a menos que", "isso não significa que") cuja ausência altere o sentido doutrinário da afirmação quando lida isoladamente.

**RN-029 — Veto por fidelidade e sinalização doutrinária**  
Candidatos cuja fidelidade ao contexto e à doutrina esteja abaixo do mínimo aceitável deverão ser reprovados independentemente da pontuação total. Candidatos que contenham afirmação doutrinária forte, controversa ou passível de leitura equivocada fora de contexto deverão ser marcados para revisão humana reforçada antes da aprovação.

## 9.4 Uso de LLM local e externo

**RN-030 — Provedor configurável**  
O pipeline deverá permitir selecionar o provedor de LLM sem alterar as regras de negócio ou o formato dos resultados.

**RN-031 — Modos de operação**  
Deverão existir os modos local, externo e híbrido, sujeitos à disponibilidade técnica e às credenciais configuradas.

**RN-032 — Contrato de saída**  
Todos os provedores deverão retornar o mesmo contrato estruturado, contendo timestamps, pontuação, frase de abertura, justificativa e indicação de pensamento completo.

**RN-033 — Modo híbrido**  
No modo híbrido, o LLM local deverá gerar candidatos iniciais e o LLM externo deverá revisar a transcrição completa, reclassificar, rejeitar ou acrescentar candidatos.

**RN-034 — Falha do modelo externo**  
Se o modelo externo estiver indisponível, o sistema poderá continuar com o modelo local somente quando a política do processamento permitir; a ocorrência deverá ser informada ao operador.

**RN-035 — Falha do modelo local**  
Se o modelo local estiver indisponível, o sistema poderá usar o provedor externo somente quando houver configuração e autorização para envio da transcrição.

**RN-036 — Registro da análise**  
Cada execução deverá registrar provedor, modelo, versão ou identificador disponível, modo de operação, versão do prompt e data da análise.

**RN-037 — Dados enviados externamente**  
Por padrão, o provedor externo deverá receber apenas a transcrição e os metadados necessários. O vídeo completo não deverá ser enviado sem decisão explícita.

**RN-038 — Segredos**  
Chaves de API e demais credenciais não poderão aparecer em logs, relatórios, arquivos de saída ou mensagens destinadas ao usuário.

## 9.5 Geração e classificação dos candidatos

**RN-040 — Quantidade inicial**  
A análise deverá produzir quantidade configurável de candidatos; recomenda-se de cinco a dez opções antes da classificação final.

**RN-041 — Duração**  
O Short deverá possuir duração máxima de 60 segundos no MVP, tendo 58 segundos como duração-alvo. Excepcionalmente, poderá possuir menos de 50 segundos quando um trecho menor expressar uma mensagem completa, clara, autossuficiente e fiel ao contexto do sermão. O sistema deverá priorizar a integridade da mensagem sobre o preenchimento artificial da duração.

**RN-042 — Pensamento completo**  
O trecho deverá iniciar e terminar em limites naturais de fala, sem interromper frase, raciocínio, aplicação ou citação bíblica.

**RN-043 — Compreensão isolada**  
O candidato deverá ser compreensível por uma pessoa que não assistiu ao restante do culto ou do sermão.

**RN-044 — Abertura relevante**  
Os primeiros segundos deverão apresentar uma frase, pergunta, afirmação ou transição capaz de despertar interesse sem apelar para distorção ou sensacionalismo.

**RN-045 — Timestamps existentes**  
O LLM somente poderá utilizar horários presentes na transcrição. Timestamps inventados, invertidos ou fora do intervalo deverão invalidar o candidato.

**RN-046 — Não duplicidade**  
Candidatos com sobreposição substancial ou conteúdo equivalente deverão ser consolidados, preservando-se o mais bem classificado.

**RN-047 — Justificativa**  
Cada candidato deverá explicar por que funciona como Short e como se relaciona com o tema da pregação.

**RN-048 — Lista final**  
O sistema deverá apresentar pelo menos os três melhores candidatos válidos, quando houver material suficiente.

## 9.6 Critérios de pontuação

| **Peso** | **Critério**             | **Interpretação**                                                                 |
|----------|--------------------------|-----------------------------------------------------------------------------------|
| 30%      | Fidelidade e contexto    | Preserva a intenção do pregador e o sentido dentro do sermão.                     |
| 30%      | Valor bíblico e pastoral | Apresenta ensino, aplicação, encorajamento, confronto ou esperança de modo claro. |
| 20%      | Completude e autonomia   | Pode ser compreendido isoladamente e contém pensamento completo.                  |
| 10%      | Força da abertura        | Inicia com conteúdo capaz de manter a atenção sem sensacionalismo.                |
| 10%      | Adequação ao formato     | Encaixa-se na duração e possui limites naturais para edição.                      |

Cada critério é pontuado de 0 até o valor do seu peso, e a nota final é a soma dos cinco, resultando em uma escala de 0 a 100. A pontuação serve para ordenar os candidatos, não para aprová-los: conforme a RN-029, um candidato cuja fidelidade ao contexto e à doutrina esteja abaixo do mínimo aceitável deverá ser reprovado independentemente da nota total. Os pesos priorizam ensino e fidelidade sobre engajamento, refletindo o propósito de edificação da igreja.

**RN-049 — Pontuação explicável**  
A nota final deverá ser acompanhada das notas por critério ou de justificativa equivalente, permitindo comparação entre candidatos.

## 9.7 Revisão e aprovação

**RN-050 — Prévia obrigatória**  
Os candidatos finais deverão ser disponibilizados para reprodução antes da aprovação.

**RN-051 — Aprovação humana**  
Nenhum Short deverá ser considerado pronto para publicação sem aprovação explícita do responsável definido pela igreja.

**RN-052 — Rejeição**  
O aprovador poderá rejeitar um candidato, selecionar outro ou solicitar ajuste de início e fim, reutilizando o vídeo retido enquanto o sermão estiver em aberto, sem novo download.

**RN-053 — Registro da decisão**  
A aprovação ou rejeição deverá registrar candidato, responsável, data e eventual justificativa.

## 9.8 Formatação visual e enquadramento

**RN-060 — Formato vertical**  
O Short final deverá utilizar proporção 9:16, preferencialmente em resolução 1080 × 1920 pixels.

**RN-061 — Preservação da imagem**  
O vídeo não poderá ser esticado ou deformado para preencher o quadro vertical.

**RN-062 — Sujeito principal**  
O enquadramento deverá manter o pregador como sujeito principal sempre que ele estiver visível na fonte.

**RN-063 — Estratégia de reenquadramento**  
O sistema deverá suportar ao menos enquadramento fixo configurável. Reenquadramento inteligente e fundo desfocado poderão ser acrescentados como evolução.

**RN-064 — Validação visual**  
Quando o recorte automático ocultar texto projetado relevante ou outro elemento essencial, o candidato deverá exigir ajuste manual antes da renderização final. O pregador momentaneamente fora do enquadramento fixo é aceitável e não bloqueia a renderização, pois ele é orientado a permanecer em close.

## 9.9 Legendas e identidade visual

**RN-070 — Legenda incorporada**  
O vídeo final deverá conter legendas visíveis incorporadas à imagem, independentemente das legendas opcionais da plataforma.

**RN-071 — Sincronização**  
O início e o fim de cada legenda deverão acompanhar a fala do trecho aprovado, sem deslocamento perceptível.

**RN-072 — Perfil visual versionado**  
Fonte, tamanho, cores, contorno, sombra, posição e margens deverão ser definidos em perfil reutilizável e versionado.

**RN-073 — Legibilidade**  
As legendas deverão permanecer dentro da área segura, apresentar contraste suficiente e evitar cobrir o rosto do pregador.

**RN-074 — Quantidade de linhas**  
A apresentação no MVP deverá usar blocos curtos, com no máximo duas linhas por vez, salvo exceção justificada pelo perfil visual. A exibição palavra por palavra fica prevista para a Fase 2.

**RN-075 — Fonte textual**  
A primeira versão poderá usar os segmentos da legenda do YouTube. O alinhamento palavra por palavra poderá utilizar transcrição local apenas sobre o trecho aprovado.

**RN-076 — Correção editorial**  
O operador deverá poder corrigir erros evidentes da legenda antes da renderização, sem alterar o conteúdo falado. A revisão de nomes bíblicos, referências (livro, capítulo e versículo) e citações da Escritura presentes no trecho aprovado deverá ser obrigatória antes da incorporação da legenda.

## 9.10 Renderização, entrega e histórico

**RN-080 — Formato de entrega**  
O arquivo final deverá ser gerado em MP4, com vídeo e áudio compatíveis com publicação no YouTube.

**RN-081 — Sincronia audiovisual**  
O recorte e a conversão não deverão introduzir dessincronização perceptível entre áudio, vídeo e legenda.

**RN-082 — Identificação do arquivo**  
O nome ou metadado do resultado deverá permitir relacionar o Short ao vídeo original, ao candidato aprovado e à versão gerada.

**RN-083 — Não sobrescrita**  
Uma nova renderização não deverá substituir silenciosamente uma versão anteriormente aprovada; os resultados deverão ser versionados.

**RN-084 — Reprocessamento**  
Etapas concluídas e válidas deverão ser reaproveitadas sempre que possível, evitando novo download ou nova análise sem necessidade.

**RN-085 — Estado de conclusão**  
O processamento somente será considerado concluído quando o arquivo final existir, passar pelas validações técnicas e estiver associado à aprovação correspondente.

**RN-086 — Retenção e descarte do vídeo**  
O vídeo baixado deverá ser retido enquanto o sermão de origem permanecer em aberto, permitindo gerar novos Shorts sem novo download. Ao encerrar o sermão, o vídeo deverá ser descartado. Transcrições, resultados intermediários, registros e logs deverão ser retidos após o encerramento; o prazo de retenção será definido posteriormente.

## 9.11 Falhas e exceções

**RN-090 — Ausência de legenda**  
Quando nenhuma legenda aceitável estiver disponível, o sistema deverá interromper o fluxo e informar o motivo ao operador. O MVP não realizará transcrição local.

**RN-091 — Resposta inválida do LLM**  
Respostas fora do contrato deverão ser rejeitadas. O sistema poderá realizar uma tentativa de correção e, em seguida, usar outro provedor ou solicitar intervenção.

**RN-092 — Candidatos insuficientes**  
Se não existirem três candidatos válidos, o sistema deverá apresentar os disponíveis e informar que o sermão não produziu a quantidade esperada.

**RN-093 — Falha de renderização**  
Uma falha na produção do vídeo não deverá invalidar a análise nem a aprovação já realizadas; o processamento deverá poder ser retomado.

**RN-094 — Comunicação da falha**  
Toda falha deverá indicar etapa, motivo compreensível, possibilidade de repetição e ação esperada do operador.

# 10. Estados do processamento

| **Estado**            | **Significado**                                                |
|-----------------------|----------------------------------------------------------------|
| Recebido              | URL e parâmetros registrados.                                  |
| Conteúdo obtido       | Vídeo e legenda disponíveis localmente.                        |
| Transcrição preparada | Intervalo da pregação filtrado e normalizado.                  |
| Em análise            | Um ou mais LLMs processam a transcrição completa.              |
| Aguardando aprovação  | Candidatos válidos e prévias disponíveis.                      |
| Aprovado              | Um candidato foi selecionado pelo responsável.                 |
| Em renderização       | Vídeo vertical e legendas estão sendo produzidos.              |
| Concluído             | Arquivo final validado e entregue.                             |
| Falhou                | Uma etapa não pôde continuar e exige repetição ou intervenção. |

# 11. Requisitos não funcionais essenciais

- Reprodutibilidade: os mesmos parâmetros deverão permitir rastrear como um resultado foi obtido.

- Observabilidade: cada etapa deverá registrar início, fim, estado, duração e falhas, sem expor credenciais.

- Portabilidade: os componentes principais deverão poder ser executados em ambiente Linux e empacotados em contêineres quando necessário.

- Substituição de provedor: a troca entre LLM local e externo não deverá exigir alteração das regras de seleção.

- Privacidade: o envio a serviços externos deverá ser limitado ao conteúdo necessário e depender de configuração explícita.

- Recuperação: o pipeline deverá retomar etapas posteriores sem repetir aquisições e análises válidas.

- Retenção: o vídeo deverá ser mantido apenas enquanto o sermão estiver em aberto e descartado ao encerrá-lo; transcrições, resultados e logs deverão ser retidos, com prazo a definir.

# 12. Critérios de aceite do MVP

| **ID** | **Critério**                                                                                                    |
|--------|-----------------------------------------------------------------------------------------------------------------|
| CA-001 | Aceitar URL e horários válidos de início e fim da pregação.                                                     |
| CA-002 | Baixar o vídeo e a legenda em português disponível para a fonte.                                                |
| CA-003 | Produzir transcrição normalizada somente do intervalo informado, preservando timestamps.                        |
| CA-004 | Enviar a transcrição completa da pregação ao LLM selecionado.                                                   |
| CA-005 | Gerar mapa do sermão e candidatos em contrato estruturado validável.                                            |
| CA-006 | Suportar pelo menos os modos local e externo; o híbrido poderá ser ativado quando ambos estiverem configurados. |
| CA-007 | Apresentar até três melhores candidatos com duração, pontuação, abertura e justificativa.                       |
| CA-008 | Permitir aprovação humana e ajuste dos limites do candidato.                                                    |
| CA-009 | Gerar MP4 vertical 9:16, preferencialmente 1080 × 1920, sem deformação.                                         |
| CA-010 | Incorporar legenda personalizada, legível e sincronizada ao vídeo.                                              |
| CA-011 | Preservar vínculo entre fonte, análise, candidato aprovado e arquivo final.                                     |
| CA-012 | Permitir repetir a renderização e gerar novos Shorts do mesmo sermão sem baixar novamente o vídeo, enquanto o sermão estiver em aberto.        |

# 13. Decisões resolvidas

| **ID** | **Resolução**                                                                                                                              |
|--------|--------------------------------------------------------------------------------------------------------------------------------------------|
| DP-001 | Não haverá transcrição local no MVP. Sem legenda aceitável, o fluxo é interrompido.                                                        |
| DP-002 | Modelo local: Gemma 4 26B, servido localmente. Provedor externo: Gemma 4 (31B) via OpenRouter. Configuração e credenciais ficam fora do BRD. |
| DP-003 | Modo híbrido é opcional, decidido por processamento.                                                                                       |
| DP-004 | Os três finalistas recebem prévia leve (clipe recortado, sem estilização); a renderização completa ocorre apenas no candidato aprovado.    |
| DP-005 | Perfil visual definido (assets disponíveis), versionado conforme a RN-072.                                                                 |
| DP-006 | Legenda em blocos curtos (máx. duas linhas) no MVP. Exibição palavra por palavra fica para a Fase 2.                                       |
| DP-007 | Vídeo retido só enquanto o sermão estiver em aberto e descartado ao encerrá-lo. Texto, transcrições, resultados e logs retidos; prazo a definir. |
| DP-008 | Aprovação por um pastor, nível único, via grupo específico no WhatsApp.                                                                    |
| DP-009 | Enquadramento fixo; pregador orientado a permanecer em close; sair do quadro é aceitável e não bloqueia a renderização.                    |

# 14. Evolução sugerida

| **Fase** | **Escopo indicativo**                                                                                                                          |
|----------|------------------------------------------------------------------------------------------------------------------------------------------------|
| MVP      | Intervalo manual; legenda do YouTube; LLM local/externo; três candidatos; aprovação humana; recorte vertical fixo; legenda incorporada.        |
| Fase 2   | Modo híbrido aprimorado; alinhamento palavra por palavra; reenquadramento inteligente; perfis visuais adicionais; melhor interface de revisão. |
| Fase 3   | Detecção automática da pregação; análise multimodal dos candidatos; geração de metadados; integração opcional com publicação agendada.         |

# Apêndice A — Contrato mínimo de candidato

A representação abaixo é ilustrativa. A especificação técnica poderá acrescentar campos, mas deverá preservar o significado mínimo estabelecido pelas regras de negócio.

```json
{
  "start": "01:14:22.400",
  "end": "01:15:20.800",
  "duration_seconds": 58.4,
  "score": 90,
  "hook": "Frase inicial do trecho",
  "reason": "Pensamento completo e alinhado ao tema central.",
  "complete_thought": true,
  "criteria": {
    "context_fidelity": 28,
    "pastoral_value": 27,
    "completeness": 18,
    "opening_strength": 8,
    "format_fit": 9
  }
}
```

# Apêndice B — Diretrizes mínimas do prompt especialista

- Ler a transcrição completa antes de selecionar trechos.

- Usar somente timestamps existentes na transcrição.

- Priorizar fidelidade ao contexto, pensamento completo e clareza isolada.

- Evitar sensacionalismo e não modificar a intenção do pregador para aumentar engajamento.

- Explicar a relação entre cada candidato e o tema central do sermão.

- Responder somente no contrato estruturado solicitado.

> **Próximo passo:** Após a validação deste BRD, elaborar a Especificação Funcional do MVP, o desenho de arquitetura e o backlog de implementação em entregas pequenas e incrementais.