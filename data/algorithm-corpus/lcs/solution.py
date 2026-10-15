a=input().strip(); b=input().strip(); dp=[0]*(len(b)+1)
for x in a:
    prev=0
    for j,y in enumerate(b,1):
        old=dp[j]; dp[j]=prev+1 if x==y else max(dp[j],dp[j-1]); prev=old
print(dp[-1])
