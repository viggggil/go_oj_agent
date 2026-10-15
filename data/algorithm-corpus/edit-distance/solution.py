a=input().strip(); b=input().strip(); dp=list(range(len(b)+1))
for i,x in enumerate(a,1):
    row=[i]
    for j,y in enumerate(b,1): row.append(min(dp[j]+1,row[-1]+1,dp[j-1]+(x!=y)))
    dp=row
print(dp[-1])
