n,m=map(int,input().split()); dp=[0]*m; dp[0]=1
for _ in range(n):
    row=list(map(int,input().split()))
    for j,x in enumerate(row):
        if x: dp[j]=0
        elif j: dp[j]=(dp[j]+dp[j-1])%1000000007
print(dp[-1])
