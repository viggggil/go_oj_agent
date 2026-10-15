n=int(input()); a=list(map(int,input().split())); nodes=[[x,None] for x in a]
for i in range(n-1): nodes[i][1]=nodes[i+1]
prev=None; cur=nodes[0]
while cur:
    nxt=cur[1]; cur[1]=prev; prev=cur; cur=nxt
ans=[]
while prev: ans.append(prev[0]); prev=prev[1]
print(*ans)
