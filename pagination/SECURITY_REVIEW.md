# Análise de segurança de pagination

Código analisado: `fda9c68949633a85b399f34d1e0dc8419a0f8a9e` (main em 05/11/2026).
Escopo: montagem de filtros, grupos, ordenação, parâmetros e escapamento de LIKE.

## Conclusão

Com campos e operadores definidos pelo código da aplicação, os valores textuais
testados permanecem separados do SQL. `EscapeLike` escapa `%`, `_` e `!`, e o
builder adiciona `ESCAPE '!'`: as buscas literais testadas funcionam em SQLite.
Isso não torna campos, operadores ou ordenações provenientes de requisições seguros.

Foram reproduzidos três caminhos de injeção em banco SQLite isolado. A exploração
em uma aplicação depende de ela permitir que uma entrada não confiável controle
essas partes da consulta. Não foram testadas aplicações consumidoras ou produção.

## Achados

| Prioridade | Entrada | Evidência | Consequência e condição |
| --- | --- | --- | --- |
| Alta | `Filter.Field` | `filterExpr`, gorm_scope.go:73–84, interpola o campo | Se controlado externamente, permite alterar o predicado WHERE. A demonstração retorna as 6 linhas ao invés de nenhuma. |
| Alta | `Filter.Op` | O ramo `default` em gorm_scope.go:83–84 aceita qualquer `Operator(string)` | Se controlado externamente, permite inserir operadores e predicados arbitrários. Também reproduzido retornando todas as linhas. |
| Alta | `Sort.Field`, inclusive `ParseSort` | `applySorts`, gorm_scope.go:115–121, passa texto diretamente a `db.Order` | Uma subconsulta escalar foi aceita e executada no ORDER BY. Validar apenas ASC/DESC não protege o campo. |
| Média | Exemplo completo do README | `TransactionHandler.List` passa `ParseSort(req.Sort)` diretamente para `NewQuery` | O exemplo ignora a lista de campos permitidos apresentada em outra seção e pode propagar a vulnerabilidade para aplicações. |
| Compatibilidade | LIKE no SQL Server | `EscapeLike("[ab]")` mantém os colchetes | SQL Server interpreta `[ab]` como classe de caracteres. A promessa de busca literal não cobre esse dialeto integralmente. Achado de análise estática e documentação; não executado em SQL Server. |

### Reproduções sem efeitos destrutivos

As duas primeiras entradas geram uma comparação verdadeira e retornam todas as
linhas da tabela temporária usada nos testes:

```go
Filter{
    Field: "description = description OR 1=1 OR description",
    Op: Eq,
    Value: "no such description",
}

Filter{
    Field: "description",
    Op: Operator("= description OR 1=1 OR description ="),
    Value: "no such description",
}
```

Para ordenação, `ParseSort("(SELECT/**/1)")` aceita uma subconsulta sem espaços
ou vírgulas. O resultado contém `ORDER BY (SELECT/**/1) ASC` e executa em SQLite.
Isso comprova controle da expressão SQL; não depende de o driver aceitar múltiplos
comandos separados por ponto e vírgula.

### Valores e expressões GORM

Os testes inspecionam `Statement.SQL` e `Statement.Vars`, em vez de concluir
segurança a partir do texto interpolado por `ToSQL`. Dez entradas, incluindo
aspas, comentários, comandos SQL como texto, Unicode e NUL, são verificadas em
oito operadores escalares. Para LIKE/ILIKE, também se verifica o parâmetro escapado.
ILIKE é inspecionado na montagem do SQL; não é executado em SQLite.

`IN` e `NOT IN` mantêm cada string da lista como parâmetro e têm resultados
verificados no banco. Grupos aninhados não permitem que os valores textuais
testados ultrapassem um filtro externo de status. Direções inválidas são
normalizadas para ASC; `desc` é normalizado para DESC.

Há uma ressalva ao contrato `Value any`: `gorm.Expr` é código SQL confiável e
pode ser expandido pelo GORM nos operadores que repassam o valor diretamente.
Um teste demonstra essa fronteira. Uma string JSON comum não se transforma em
`gorm.Expr`; não se trata de uma falha no binding de strings.

## Testes adicionados

`security_test.go` contém nove funções de teste e um fuzz target:

| Teste | Verificação |
| --- | --- |
| `TestSecurityScalarValuesRemainBound` | SQL invariável e argumentos separados para oito operadores e dez entradas |
| `TestSecurityListValuesRemainBound` | Binding e execução de IN/NOT IN com entrada maliciosa |
| `TestSecurityLikeMatchesLiteralInput` | Resultado literal para curingas, escape, aspas, SQL como texto, barras e Unicode |
| `TestSecurityScopeNestedValuesCannotBypassOuterFilter` | Grupos aninhados, total e linhas retornadas por Scope |
| `TestSecuritySortDirectionsCannotInjectSQL` | Normalização restrita das direções |
| `TestSecurityDemonstrationFilterFieldCanInjectPredicate` | Reprodução da injeção por campo |
| `TestSecurityDemonstrationOperatorCanInjectPredicate` | Reprodução da injeção por operador |
| `TestSecurityDemonstrationSortFieldsAcceptSQLExpressions` | Reprodução via ParseSort, SortBuilder e Sort literal |
| `TestSecurityDemonstrationExpressionValuesAreTrustedCode` | Tratamento especial de gorm.Expr |
| `FuzzSecurityEscapeLikeSQLite` | Propriedade de correspondência literal do padrão escapado, sem curingas externos |

**Os testes `Demonstration` passam quando reproduzem o comportamento vulnerável.**
São evidências de caracterização, não proteções contra regressão. Depois de
implementar validação, devem ser substituídos por asserções de rejeição.

## Como executar

Resultado local: suíte completa aprovada (26 testes e as 9 sementes do fuzz
target), execução com `-race` aprovada e cobertura total do pacote de 91,8%.
Fuzzing de 15 segundos: 21.978 execuções, sem falhas na propriedade testada.
Ambiente: Go 1.25.0, GORM 1.31.1, driver SQLite 1.6.0, go-sqlite3 1.14.22.

Requer Go compatível com go.mod e um compilador C para go-sqlite3. Nenhuma
dependência foi adicionada. Os testes usam SQLite em memória.

```bash
go test ./pagination -count=1
go test ./pagination -run '^TestSecurity' -count=1 -v
go test ./pagination -race -cover -count=1
go test ./pagination -run '^$' -fuzz '^FuzzSecurityEscapeLikeSQLite$' -fuzztime=15s -parallel=2
```

O fuzz target usa UTF-8 válido sem NUL e até 4096 bytes. SQLite LIKE para no NUL;
esse caso fica fora da propriedade de correspondência literal, embora o binding
de NUL seja inspecionado nos testes de montagem. Os testes não estabelecem
igualdade de comportamento entre collations ou dialetos.

## Correções recomendadas

1. Mapear nomes externos para uma lista explícita de colunas permitidas por
   endpoint, tanto em filtros quanto em ordenações. Rejeitar campos desconhecidos
   antes de executar a consulta. Campos sintaticamente válidos também podem ser
   indevidos para aquele endpoint.
2. Validar `Operator` contra a lista de constantes suportadas, inclusive dentro
   de grupos. O ramo `default` deve registrar um erro no GORM e impedir execução;
   simplesmente ignorar um filtro inválido pode ampliar os resultados.
3. Usar identificadores estruturados do GORM, como `clause.Column` e
   `clause.OrderByColumn`, após a validação. Não tentar escapar nomes de colunas
   com `EscapeLike` nem com substituição de aspas. Preservar uma API explícita
   para expressões confiáveis se joins ou funções SQL forem necessários.
4. Corrigir o exemplo completo do README para aplicar a lista de campos
   permitidos e verificar erros da consulta.
5. Manter valores como parâmetros; `EscapeLike` não é um sanitizador SQL geral.
   Para uso no SQL Server, tratar também `[` com uma estratégia de escape
   validada no próprio dialeto e adicionar integração específica.
6. Acrescentar integração PostgreSQL para ILIKE e validação no banco realmente
   usado pelas aplicações. Os testes SQLite não comprovam compatibilidade de
   PostgreSQL, MySQL ou SQL Server.

Não houve alteração no código de produção nesta análise.

## Referências

- [Código analisado](https://github.com/raykavin/gobox/tree/fda9c68949633a85b399f34d1e0dc8419a0f8a9e/pagination)
- [Segurança no GORM](https://gorm.io/docs/security.html)
- [LIKE no SQL Server](https://learn.microsoft.com/en-us/sql/t-sql/language-elements/like-transact-sql?view=sql-server-ver17)
