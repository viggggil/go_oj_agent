n,m=map(int,input().split()); coins=list(map(int,input().split())); dp=[0]+[m+1]*m
for s in range(1,m+1):
    for c in coins:
        if c<=s: dp[s]=min(dp[s],dp[s-c]+1)
print(dp[m] if dp[m]<=m else -1)
