# Rinha de Backend 2025

Este repositório contém a minha solução para a Rinha de Backend 2025 utilizando Go.

Essa subimissão faz parte do meu aprendizado sobre Go, Redis, e filas de processamento assíncrono.

## Tecnologias utilizadas

- **Nginx** - Load balancer
- **Redis** - Stream das requisições de pagamentos e armazenamento de pagamentos processados.

## Estratégia

Responder o mais rápido possível às requisições de pagamento enfileirando os pagamentos para **processar de forma assíncrona**.

Fazer o processamento sem erros ou inconsistências **para evitar multas**.

Armazenar pagamentos processados em um **Set ordenado pela timestamp no Redis** para facilicar a busca para sumarizar.

## Execução

Utilizo uma Stream do Redis para armazenar as requisições de pagamentos e ReaderGroups para ler pagamentos da Stream para processamento.

Uma goroutine verifica constantemente a saúde das APIs de processamento. Com base no status e no tempo de resposta (levando em conta um *bias* em favor da Default) escolhe qual a melhor API a ser utilizada.

Cria N workers para ler da Stream e processar os pagamentos. Cada worker possui o seu próprio consumerId para ler da Stream.

O worker verifica qual é a melhor API a se utilizar antes de fazer q requisição. Se falhar, ele tenta novamente, se possível (erro 5xx ou 429), após um breve mas crescente backoff.

Após receber uma resposta de sucesso da API de processamento, o worker armazena o pagamento em um set ordenado pela timestamp no Redis referente a API utilizada.

Para responder requisições sobre o sumário, consulto a range dada pelas timestamps de início e fim (to, from) para o set da api Default e Fallback e faço a soma do Amount e número de pagamentos processados.

## Melhorias Possíveis

### Lógica de seleção de qual é a melhor API

A lógica utilizada atualmente é bastante simples. Acredito que seja possível melhorar a predição de qual é a melhor API a se usar se começarmos a olhar para o resultado das requisições feitas pelos workers (status e tempo de resposta).

Com isso, a API poderia se tornar mais independente das requisições de *health-check* e mais ágil para alternar entre as APIs.

### Lógica de retry para processamentos

A lógica atual é bastante simplista e basicamente descarta uma requisição de pagamentos se esta não for processada dentro do tempo limite das retries com backoff.

Se as APIs de processamento ficarem muito tempo fora do ar, a solução que implementei vai acabar perdendo vários pagamentos neste meio tempo.

Uma solução possível seria adicionar estes pagamentos a uma nova fila e processa-los quando possível. Outra solução seria simplesmente não ter um limite de retries para respostas que não indiquem um problema com a requisição, porém correndo um risco de deadlock.

## Aprendizados

- Pude aprender brevemente sobre os diferentes tipos de dados que podem ser amazenados no Redis e como cada um deles pode ser útil
  - Em especial Streams e Sets.
- Aprendi sobre estratégias de processamento assíncrono com workers.
- Estratégias para sincronização de sistemas distribuídos
