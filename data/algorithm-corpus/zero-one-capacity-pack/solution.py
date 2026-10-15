n,c=map(int,input().split()); dp=[0]*(c+1)
for _ in range(n):
    w,v=map(int,input().split())
    for j in range(c,w-1,-1): dp[j]=max(dp[j],dp[j-w]+v)
print(dp[c])
